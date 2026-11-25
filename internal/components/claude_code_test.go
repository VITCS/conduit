package components

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"conduit/internal/model"
)

// stubClaude writes a fake `claude` CLI that speaks the stream-json protocol. It
// records its argv, environment and stdin under dir, and behaves according to the
// file dir/mode: "plan" replies with a change plan; "apply" edits app.txt;
// "apply-extra" also creates an unapproved file; "fail" exits without a result.
func stubClaude(t *testing.T, dir string) string {
	t.Helper()
	script := `#!/usr/bin/env bash
D='` + dir + `'
printf '%s\n' "$@" > "$D/args"
env > "$D/env"
cat > "$D/stdin"
mode=$(cat "$D/mode" 2>/dev/null)
echo '{"type":"system","subtype":"init"}'
case "$mode" in
  plan)
    echo '{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"'"$PWD"'/app.txt"}}]}}'
    echo '{"type":"result","subtype":"success","is_error":false,"num_turns":3,"total_cost_usd":0.02,"result":"Add a greeting line to app.txt and cover it in a test.\n<change_plan>[{\"path\":\"app.txt\",\"action\":\"modify\",\"details\":\"add greeting\"},{\"path\":\"app_test.txt\",\"action\":\"create\",\"details\":\"test it\"}]</change_plan>"}'
    ;;
  apply|apply-extra)
    echo "hello from claude" >> app.txt
    echo "test" > app_test.txt
    [ "$mode" = apply-extra ] && echo "oops" > secret_notes.txt
    echo '{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"'"$PWD"'/app.txt"}}]}}'
    echo '{"type":"result","subtype":"success","is_error":false,"num_turns":5,"result":"Added the greeting and a test."}'
    ;;
  check)
    echo '{"type":"result","subtype":"success","is_error":false,"num_turns":1,"result":"OK"}'
    ;;
  *) echo "boom" >&2; exit 1 ;;
esac
`
	bin := filepath.Join(dir, "claude")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// bareRepo creates a bare git repository with one commit on main (app.txt).
func bareRepo(t *testing.T, dir string) string {
	t.Helper()
	bare := filepath.Join(dir, "origin.git")
	work := filepath.Join(dir, "seed")
	for _, c := range [][]string{
		{"init", "-q", "--bare", "-b", "main", bare},
		{"init", "-q", "-b", "main", work},
	} {
		if out, err := exec.Command("git", c...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", c, err, out)
		}
	}
	os.WriteFile(filepath.Join(work, "app.txt"), []byte("app\n"), 0o644)
	for _, c := range [][]string{
		{"add", "."}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "init"},
		{"remote", "add", "origin", bare}, {"push", "-q", "origin", "main"},
	} {
		cmd := exec.Command("git", c...)
		cmd.Dir = work
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", c, err, out)
		}
	}
	return bare
}

type claudeFixture struct {
	dir, bare string
	vars      map[string]string
	secrets   map[string]string
}

func newClaudeFixture(t *testing.T) *claudeFixture {
	t.Helper()
	for _, b := range []string{"git", "bash"} {
		if _, err := exec.LookPath(b); err != nil {
			t.Skip(b + " not available")
		}
	}
	f := &claudeFixture{dir: t.TempDir(), vars: map[string]string{}, secrets: map[string]string{}}
	f.bare = bareRepo(t, f.dir)
	t.Setenv("CONDUIT_CLAUDE_BIN", stubClaude(t, f.dir))
	t.Setenv("CONDUIT_SECRET_KEY", "server-secret-must-not-leak")
	t.Setenv("CONDUIT_CLAUDE_SERVER_LOGIN", "")
	oldV, oldS := ResolveVar, ResolveSecret
	ResolveVar = func(_, n string) (string, bool) { v, ok := f.vars[n]; return v, ok }
	ResolveSecret = func(_, n string) (string, bool) { v, ok := f.secrets[n]; return v, ok }
	t.Cleanup(func() { ResolveVar, ResolveSecret = oldV, oldS })
	f.vars["coding_engine"] = "claude_code"
	f.vars["owner"], f.vars["repo"] = "acme", "web"
	f.secrets["claude_code_oauth_token"] = "sk-ant-oat-test"
	return f
}

func (f *claudeFixture) mode(t *testing.T, m string) {
	if err := os.WriteFile(filepath.Join(f.dir, "mode"), []byte(m), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *claudeFixture) read(name string) string {
	b, _ := os.ReadFile(filepath.Join(f.dir, name))
	return string(b)
}

func (f *claudeFixture) node(typ string, extra ...model.Field) model.Node {
	cfg := []model.Field{
		{Key: "owner", Value: "${var:owner}"}, {Key: "repo", Value: "${var:repo}"},
		{Key: "clone_url", Value: f.bare}, {Key: "token", Value: "ghp-test-token"},
		{Key: "ticket", Value: "ENG-1"}, {Key: "plan", Value: "Add a greeting"},
	}
	return model.Node{ID: typ, Type: typ, Config: append(cfg, extra...)}
}

func TestCodingEngineDefaultsToAPI(t *testing.T) {
	old := ResolveVar
	t.Cleanup(func() { ResolveVar = old })
	vars := map[string]string{}
	ResolveVar = func(_, n string) (string, bool) { v, ok := vars[n]; return v, ok }
	if got := codingEngine(model.Node{}, "p"); got != engineAPI {
		t.Fatalf("unset engine = %q, want api", got)
	}
	vars["coding_engine"] = "Claude-Code"
	if got := codingEngine(model.Node{}, "p"); got != engineClaudeCode {
		t.Fatalf("coding_engine=Claude-Code → %q", got)
	}
	n := model.Node{Config: []model.Field{{Key: "engine", Value: "api"}}}
	if got := codingEngine(n, "p"); got != engineAPI {
		t.Fatalf("node field must override the project variable, got %q", got)
	}
}

func TestClaudeAuthModes(t *testing.T) {
	oldV, oldS := ResolveVar, ResolveSecret
	t.Cleanup(func() { ResolveVar, ResolveSecret = oldV, oldS })
	vars, secrets := map[string]string{}, map[string]string{}
	ResolveVar = func(_, n string) (string, bool) { v, ok := vars[n]; return v, ok }
	ResolveSecret = func(_, n string) (string, bool) { v, ok := secrets[n]; return v, ok }
	t.Setenv("CONDUIT_CLAUDE_SERVER_LOGIN", "")

	if _, _, msg := claudeAuth("p"); msg == "" {
		t.Fatal("no credentials must explain what to set")
	}
	secrets["llm_key"] = "sk-ant-api"
	if m, env, _ := claudeAuth("p"); m != authAPIKey || env[0] != "ANTHROPIC_API_KEY=sk-ant-api" {
		t.Fatalf("llm_key with Anthropic provider → %q %v", m, env)
	}
	vars["llm_provider"] = "openai"
	if _, _, msg := claudeAuth("p"); msg == "" {
		t.Fatal("an OpenAI llm_key must not be sent to Claude Code")
	}
	secrets["claude_code_oauth_token"] = "oat"
	if m, env, _ := claudeAuth("p"); m != authSubscription || env[0] != "CLAUDE_CODE_OAUTH_TOKEN=oat" {
		t.Fatalf("subscription token must win in auto mode, got %q %v", m, env)
	}
	secrets["anthropic_api_key"] = "sk-ant-explicit"
	vars["claude_auth"] = "api_key"
	if m, env, _ := claudeAuth("p"); m != authAPIKey || env[0] != "ANTHROPIC_API_KEY=sk-ant-explicit" {
		t.Fatalf("explicit api_key → %q %v", m, env)
	}
	vars["claude_auth"] = "server_login"
	if _, _, msg := claudeAuth("p"); !strings.Contains(msg, "CONDUIT_CLAUDE_SERVER_LOGIN") {
		t.Fatalf("server_login must be refused unless the operator allows it: %q", msg)
	}
	t.Setenv("CONDUIT_CLAUDE_SERVER_LOGIN", "true")
	if m, env, msg := claudeAuth("p"); m != authServerLogin || env != nil || msg != "" {
		t.Fatalf("allowed server_login → %q %v %q", m, env, msg)
	}
}

func TestClaudePlanReadsRepoAndProposesFiles(t *testing.T) {
	f := newClaudeFixture(t)
	f.mode(t, "plan")
	var progressLines []string
	ctx := WithProgress(context.Background(), func(m string) { progressLines = append(progressLines, m) })
	res := Exec(ctx, "coding_plan", f.node("coding_plan"), &model.Run{ProjectID: "p"})
	if res.Outcome != model.StatusSuccess {
		t.Fatalf("plan failed: %v", res.Output)
	}
	plan, _ := res.MemoryWrites["change_plan"].(string)
	if !strings.Contains(plan, "Approach: Add a greeting line") || !strings.Contains(plan, "[modify] app.txt") || !strings.Contains(plan, "[create] app_test.txt") {
		t.Fatalf("change_plan = %q", plan)
	}
	if res.MemoryWrites["base_branch"] != "main" {
		t.Fatalf("default branch not detected: %v", res.MemoryWrites["base_branch"])
	}
	args := f.read("args")
	for _, want := range []string{"-p", "dontAsk", "--strict-mcp-config", "--setting-sources\nuser"} {
		if !strings.Contains(args, want) {
			t.Errorf("claude args missing %q:\n%s", want, args)
		}
	}
	if !strings.Contains(args, "Edit,Write,NotebookEdit,Bash") {
		t.Errorf("plan session must not be allowed to edit or run commands:\n%s", args)
	}
	if !strings.Contains(f.read("stdin"), "Add a greeting") {
		t.Error("the prompt must be sent on stdin")
	}
	env := f.read("env")
	if strings.Contains(env, "server-secret-must-not-leak") || strings.Contains(env, "ghp-test-token") {
		t.Fatal("server secrets or the GitHub token leaked into the Claude Code environment")
	}
	if !strings.Contains(env, "CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat-test") {
		t.Fatal("the subscription token was not passed")
	}
	if len(progressLines) == 0 || !strings.Contains(strings.Join(progressLines, "\n"), "Read app.txt") {
		t.Fatalf("tool use was not reported as progress: %v", progressLines)
	}
}

func applyNode(f *claudeFixture) model.Node {
	return f.node("coding_apply", model.Field{Key: "changes",
		Value: `[{"path":"app.txt","action":"modify","details":"add greeting"},{"path":"app_test.txt","action":"create","details":"test"}]`})
}

func TestClaudeApplyPushesOnlyApprovedFiles(t *testing.T) {
	f := newClaudeFixture(t)
	f.mode(t, "apply")
	f.vars["claude_allowed_commands"] = "npm test, git push --force"
	res := Exec(context.Background(), "coding_apply", applyNode(f), &model.Run{ProjectID: "p"})
	if res.Outcome != model.StatusSuccess {
		t.Fatalf("apply failed: %v", res.Output)
	}
	if res.MemoryWrites["work_branch"] != "conduit/ENG-1" {
		t.Fatalf("work_branch = %v", res.MemoryWrites["work_branch"])
	}
	out, err := exec.Command("git", "--git-dir", f.bare, "show", "--stat", "--format=%s", "conduit/ENG-1").CombinedOutput()
	if err != nil {
		t.Fatalf("branch was not pushed: %v %s", err, out)
	}
	if s := string(out); !strings.Contains(s, "ENG-1") || !strings.Contains(s, "app.txt") || !strings.Contains(s, "app_test.txt") {
		t.Fatalf("pushed commit = %s", s)
	}
	if d, _ := res.Output["diff"].(string); !strings.Contains(d, "+hello from claude") {
		t.Fatalf("diff missing from output: %q", d)
	}
	args := f.read("args")
	if !strings.Contains(args, "Bash(npm test:*)") {
		t.Errorf("allowed command not granted:\n%s", args)
	}
	if strings.Contains(args, "Bash(git push") && !strings.Contains(args, "--disallowedTools") {
		t.Errorf("git push must never be allowed:\n%s", args)
	}
	if strings.Contains(args, "Bash(git push --force:*)") {
		t.Errorf("a git push prefix in claude_allowed_commands must be dropped:\n%s", args)
	}
	if strings.Contains(f.read("env"), "ghp-test-token") {
		t.Fatal("GitHub token leaked into the Claude Code environment")
	}
}

func TestClaudeApplyRefusesUnapprovedFiles(t *testing.T) {
	f := newClaudeFixture(t)
	f.mode(t, "apply-extra")
	res := Exec(context.Background(), "coding_apply", applyNode(f), &model.Run{ProjectID: "p"})
	if res.Outcome != model.StatusFailed {
		t.Fatalf("an unapproved file must fail the step, got %v", res.Output)
	}
	if extra, _ := res.Output["unapproved_files"].([]string); len(extra) != 1 || extra[0] != "secret_notes.txt" {
		t.Fatalf("unapproved_files = %v", res.Output["unapproved_files"])
	}
	if out, err := exec.Command("git", "--git-dir", f.bare, "rev-parse", "--verify", "conduit/ENG-1").CombinedOutput(); err == nil {
		t.Fatalf("nothing may be pushed when unapproved files changed: %s", out)
	}
}

func TestClaudeApplyFailureIsReported(t *testing.T) {
	f := newClaudeFixture(t)
	f.mode(t, "fail")
	res := Exec(context.Background(), "coding_apply", applyNode(f), &model.Run{ProjectID: "p"})
	if res.Outcome != model.StatusFailed || !strings.Contains(res.Output["error"].(string), "boom") {
		t.Fatalf("a crashed session must fail with its error, got %v", res.Output)
	}
}

func TestClaudeAPIEngineUnchanged(t *testing.T) {
	f := newClaudeFixture(t)
	f.vars["coding_engine"] = "api"
	f.mode(t, "plan")
	res := Exec(context.Background(), "coding_plan", f.node("coding_plan"), &model.Run{ProjectID: "p"})
	if f.read("args") != "" {
		t.Fatal("the API engine must not start Claude Code")
	}
	if res.Output["engine"] == engineClaudeCode {
		t.Fatal("API engine result labelled as Claude Code")
	}
}

func TestCheckClaude(t *testing.T) {
	f := newClaudeFixture(t)
	f.mode(t, "check")
	res := CheckClaude(context.Background(), "p")
	if res["ok"] != true || res["auth"] != authSubscription || res["reply"] != "OK" {
		t.Fatalf("check = %v", res)
	}
	delete(f.secrets, "claude_code_oauth_token")
	res = CheckClaude(context.Background(), "p")
	if res["ok"] != false || res["error"] == nil {
		t.Fatalf("check without sign-in must explain the problem: %v", res)
	}
}
