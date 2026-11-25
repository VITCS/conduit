// Claude Code engine for the coding steps.
//
// A project chooses how its coding_plan / coding_apply steps work with the project
// variable coding_engine:
//
//	api          (default) one model API call per step / per file — see coding_agent.go
//	claude_code  a real Claude Code session in a fresh git checkout of the repo
//
// With claude_code, Plan changes runs Claude Code read-only: it explores the code
// and proposes the exact files to change. After the human approves that list,
// Apply changes runs Claude Code with edit access (and, if the project allows it,
// a few shell commands such as the test suite). Conduit — not the agent — then
// checks that only approved files changed, commits, and pushes the branch.
//
// Sign-in (project variable claude_auth, or automatic):
//
//	subscription  secret claude_code_oauth_token (from `claude setup-token`)
//	api_key       secret anthropic_api_key (or llm_key when llm_provider is anthropic)
//	server_login  the server's own logged-in Claude Code; only when the operator sets
//	              CONDUIT_CLAUDE_SERVER_LOGIN=true (single-user, self-hosted)
//
// Isolation: each session runs in a throwaway checkout with an empty HOME and an
// environment built from an allowlist (PATH, locale, the sign-in variable), so the
// server's own secrets (database URL, encryption keys, the GitHub token) are never
// visible to it. Permissions are deny-by-default (--permission-mode dontAsk) with
// reads and edits scoped to the checkout, no web access, no MCP servers, and shell
// commands only from the project's claude_allowed_commands list.
package components

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"conduit/internal/model"
)

const (
	engineAPI        = "api"
	engineClaudeCode = "claude_code"

	authSubscription = "subscription"
	authAPIKey       = "api_key"
	authServerLogin  = "server_login"
)

// --- project settings ---------------------------------------------------------

func projVar(project, name string) string {
	if ResolveVar == nil {
		return ""
	}
	v, _ := ResolveVar(project, name)
	return strings.TrimSpace(v)
}

func projSecret(project, name string) string {
	if ResolveSecret == nil {
		return ""
	}
	v, _ := ResolveSecret(project, name)
	return strings.TrimSpace(v)
}

// codingEngine returns the engine a project's coding steps use. A node field
// `engine` overrides the project variable coding_engine; anything unrecognised
// (including unset) means the API engine, so existing projects are unchanged.
func codingEngine(n model.Node, project string) string {
	v := strings.ToLower(strings.TrimSpace(secretExpand(project, n.Field("engine"))))
	if v == "" || strings.Contains(v, "${") {
		v = strings.ToLower(projVar(project, "coding_engine"))
	}
	switch strings.ReplaceAll(v, "-", "_") {
	case "claude_code", "claude", "claudecode":
		return engineClaudeCode
	}
	return engineAPI
}

func claudeBin() string { return firstNonEmpty(os.Getenv("CONDUIT_CLAUDE_BIN"), "claude") }

// ServerLoginAllowed reports whether the operator lets projects use the server's
// own Claude Code login (CONDUIT_CLAUDE_SERVER_LOGIN=true).
func ServerLoginAllowed() bool {
	return strings.EqualFold(os.Getenv("CONDUIT_CLAUDE_SERVER_LOGIN"), "true")
}

// claudeAPIKey is the Anthropic API key a project can use with Claude Code: the
// anthropic_api_key secret, or llm_key when the project's model provider is Anthropic.
func claudeAPIKey(project string) string {
	if k := projSecret(project, "anthropic_api_key"); k != "" {
		return k
	}
	if p := strings.ToLower(projVar(project, "llm_provider")); p == "" || p == "anthropic" {
		return projSecret(project, "llm_key")
	}
	return ""
}

// claudeAuth resolves how Claude Code signs in for a project: the mode, the
// environment variable carrying the credential (none for server_login), or a
// message explaining what to set.
func claudeAuth(project string) (mode string, env []string, errMsg string) {
	want := strings.ToLower(strings.ReplaceAll(projVar(project, "claude_auth"), "-", "_"))
	token := projSecret(project, "claude_code_oauth_token")
	key := claudeAPIKey(project)
	sub := func() (string, []string, string) {
		return authSubscription, []string{"CLAUDE_CODE_OAUTH_TOKEN=" + token}, ""
	}
	api := func() (string, []string, string) {
		return authAPIKey, []string{"ANTHROPIC_API_KEY=" + key}, ""
	}
	switch want {
	case authSubscription:
		if token == "" {
			return "", nil, "claude_auth is subscription, but the claude_code_oauth_token secret is not set — run `claude setup-token` on a machine signed in to your Claude plan and save the token in Settings"
		}
		return sub()
	case authAPIKey, "apikey", "key":
		if key == "" {
			return "", nil, "claude_auth is api_key, but no Anthropic API key is set — add an anthropic_api_key secret"
		}
		return api()
	case authServerLogin:
		if !ServerLoginAllowed() {
			return "", nil, "claude_auth is server_login, but this server does not allow it (the operator must set CONDUIT_CLAUDE_SERVER_LOGIN=true)"
		}
		return authServerLogin, nil, ""
	case "":
		switch {
		case token != "":
			return sub()
		case key != "":
			return api()
		case ServerLoginAllowed():
			return authServerLogin, nil, ""
		}
		return "", nil, "Claude Code has no sign-in for this project — add a claude_code_oauth_token secret (your Claude subscription, from `claude setup-token`) or an anthropic_api_key secret"
	}
	return "", nil, fmt.Sprintf("unknown claude_auth %q (use subscription, api_key or server_login)", want)
}

// baseEnv is the allowlisted environment every child process (git, claude) gets.
// Nothing else from the server's environment is passed through.
func baseEnv(home string) []string {
	env := []string{"HOME=" + home, "GIT_TERMINAL_PROMPT=0", "DISABLE_AUTOUPDATER=1",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1"}
	for _, k := range []string{"PATH", "LANG", "LC_ALL", "TZ", "SSL_CERT_FILE", "SSL_CERT_DIR",
		"NODE_EXTRA_CA_CERTS", "HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "https_proxy", "http_proxy", "no_proxy"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	tmp := filepath.Join(home, "tmp")
	_ = os.MkdirAll(tmp, 0o700)
	return append(env, "TMPDIR="+tmp)
}

// claudeEnv is the environment for a Claude Code session. server_login keeps the
// real HOME (where the CLI's login lives) and USER; other modes get the throwaway
// home plus exactly one credential variable.
func claudeEnv(mode string, authEnv []string, home string) []string {
	if mode == authServerLogin {
		env := baseEnv(home)
		for i, kv := range env {
			if strings.HasPrefix(kv, "HOME=") {
				if h, err := os.UserHomeDir(); err == nil {
					env[i] = "HOME=" + h
				}
			}
		}
		if u, ok := os.LookupEnv("USER"); ok {
			env = append(env, "USER="+u)
		}
		return env
	}
	return append(baseEnv(home), authEnv...)
}

// --- running Claude Code -------------------------------------------------------

type claudeSpec struct {
	dir, prompt, system, model, budget string
	allowed, disallowed                []string
	env                                []string
	timeout                            time.Duration
}

type claudeOutcome struct {
	result  string
	isError bool
	cost    float64
	usage   llmUsage
	model   string // the main model the session ran on, when reported
	turns   int
	denials []string
	stderr  string
}

func claudeArgs(s claudeSpec) []string {
	args := []string{"-p", "--output-format", "stream-json", "--verbose",
		// Deny anything not explicitly allowed, instead of waiting on a prompt.
		"--permission-mode", "dontAsk",
		// Only the (throwaway) user settings: a repository's own .claude settings
		// can't widen permissions or add hooks. Its CLAUDE.md is still read.
		"--setting-sources", "user",
		"--strict-mcp-config", "--no-session-persistence"}
	if s.system != "" {
		args = append(args, "--append-system-prompt", s.system)
	}
	if s.model != "" {
		args = append(args, "--model", s.model)
	}
	if s.budget != "" {
		args = append(args, "--max-budget-usd", s.budget)
	}
	if len(s.allowed) > 0 {
		args = append(args, "--allowedTools", strings.Join(s.allowed, ","))
	}
	if len(s.disallowed) > 0 {
		args = append(args, "--disallowedTools", strings.Join(s.disallowed, ","))
	}
	return args
}

// runClaude runs one headless Claude Code session (prompt on stdin, stream-json
// on stdout), reporting tool use as progress and returning the final result.
func runClaude(ctx context.Context, s claudeSpec) (claudeOutcome, error) {
	var out claudeOutcome
	bin := claudeBin()
	if _, err := exec.LookPath(bin); err != nil {
		return out, fmt.Errorf("Claude Code CLI %q is not installed on this server (install @anthropic-ai/claude-code or set CONDUIT_CLAUDE_BIN)", bin)
	}
	rctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	cmd := exec.CommandContext(rctx, bin, claudeArgs(s)...)
	cmd.Dir = s.dir
	cmd.Env = s.env
	cmd.Stdin = strings.NewReader(s.prompt)
	ownProcessGroup(cmd)
	cmd.WaitDelay = 5 * time.Second
	var stderr tailBuffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return out, err
	}
	if err := cmd.Start(); err != nil {
		return out, fmt.Errorf("could not start Claude Code: %w", err)
	}
	sawResult := false
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 1<<20), 32<<20)
	for sc.Scan() {
		line := sc.Bytes()
		var ev map[string]any
		if json.Unmarshal(line, &ev) != nil {
			continue
		}
		switch ev["type"] {
		case "assistant":
			for _, msg := range toolUses(ev, s.dir) {
				progress(ctx, "Claude Code · "+msg)
			}
		case "result":
			sawResult = true
			out.result, _ = ev["result"].(string)
			out.isError, _ = ev["is_error"].(bool)
			out.cost, _ = ev["total_cost_usd"].(float64)
			out.usage, out.model = resultUsage(ev)
			if t, ok := ev["num_turns"].(float64); ok {
				out.turns = int(t)
			}
			out.denials = denials(ev)
			if sub, _ := ev["subtype"].(string); sub != "" && sub != "success" && out.result == "" {
				out.result = sub
			}
		}
	}
	werr := cmd.Wait()
	out.stderr = strings.TrimSpace(stderr.String())
	switch {
	case ctx.Err() != nil:
		return out, errors.New("canceled")
	case rctx.Err() == context.DeadlineExceeded:
		return out, fmt.Errorf("Claude Code did not finish within %s (raise claude_timeout_minutes)", s.timeout)
	case !sawResult:
		msg := firstNonEmpty(lastLine(out.stderr), errString(werr), "no result")
		return out, fmt.Errorf("Claude Code stopped without a result: %s", msg)
	case out.isError:
		return out, fmt.Errorf("Claude Code reported an error: %s", firstNonEmpty(out.result, lastLine(out.stderr)))
	}
	return out, nil
}

// toolUses renders the tool calls in an assistant event as short progress lines.
func toolUses(ev map[string]any, dir string) []string {
	msg, _ := ev["message"].(map[string]any)
	content, _ := msg["content"].([]any)
	var lines []string
	for _, c := range content {
		item, _ := c.(map[string]any)
		if item["type"] != "tool_use" {
			continue
		}
		name, _ := item["name"].(string)
		in, _ := item["input"].(map[string]any)
		detail := ""
		for _, k := range []string{"file_path", "path", "pattern", "command"} {
			if v, ok := in[k].(string); ok && v != "" {
				detail = strings.TrimPrefix(strings.TrimPrefix(v, dir), "/")
				break
			}
		}
		if len(detail) > 120 {
			detail = detail[:117] + "…"
		}
		lines = append(lines, strings.TrimSpace(name+" "+detail))
	}
	return lines
}

func denials(ev map[string]any) []string {
	list, _ := ev["permission_denials"].([]any)
	var out []string
	for _, d := range list {
		m, _ := d.(map[string]any)
		name, _ := m["tool_name"].(string)
		in, _ := m["tool_input"].(map[string]any)
		cmd, _ := in["command"].(string)
		if cmd == "" {
			cmd, _ = in["file_path"].(string)
		}
		out = append(out, strings.TrimSpace(name+" "+cmd))
	}
	return out
}

// tailBuffer keeps the last 8 KiB written to it (stderr can be chatty).
type tailBuffer struct{ b []byte }

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.b = append(t.b, p...)
	if len(t.b) > 8<<10 {
		t.b = t.b[len(t.b)-8<<10:]
	}
	return len(p), nil
}
func (t *tailBuffer) String() string { return string(t.b) }

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[i+1:])
	}
	return s
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// --- git ------------------------------------------------------------------------

// gitEnv is baseEnv plus the GitHub token as an HTTP header passed through git's
// GIT_CONFIG_* variables — never on the command line, never written to the
// checkout's .git/config, and never given to the Claude Code process.
func gitEnv(home, token string) []string {
	env := baseEnv(home)
	if token != "" {
		basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
		env = append(env, "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.extraHeader",
			"GIT_CONFIG_VALUE_0=Authorization: Basic "+basic)
	}
	return env
}

func runGit(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 600 {
			msg = msg[len(msg)-600:]
		}
		return string(out), fmt.Errorf("git %s: %s", args[0], firstNonEmpty(msg, err.Error()))
	}
	return string(out), nil
}

// cloneURL is where the repository is cloned from: an explicit clone_url field,
// else derived from the GitHub API base (github.com or GitHub Enterprise).
func cloneURL(n model.Node, owner, repo string) string {
	if u := strings.TrimSpace(n.Field("clone_url")); u != "" {
		return u
	}
	base := githubBase(n)
	web := "https://github.com"
	if !strings.Contains(base, "api.github.com") {
		web = strings.TrimSuffix(strings.TrimSuffix(base, "/api/v3"), "/api")
	}
	return fmt.Sprintf("%s/%s/%s.git", web, owner, repo)
}

// defaultBranch asks the remote for its HEAD branch.
func defaultBranch(ctx context.Context, env []string, url string) (string, error) {
	out, err := runGit(ctx, "", env, "ls-remote", "--symref", url, "HEAD")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "ref: refs/heads/") {
			return strings.Fields(strings.TrimPrefix(line, "ref: refs/heads/"))[0], nil
		}
	}
	return "", errors.New("could not find the repository's default branch")
}

// checkout is a throwaway clone plus the HOME that git and Claude Code use.
type checkout struct {
	root, dir, home string
	gitEnv          []string
}

func (c *checkout) close() { _ = os.RemoveAll(c.root) }

// newCheckout clones url at branch into a fresh temporary directory.
func newCheckout(ctx context.Context, url, branch, token string) (*checkout, error) {
	root, err := os.MkdirTemp(os.Getenv("CONDUIT_WORK_DIR"), "conduit-claude-")
	if err != nil {
		return nil, err
	}
	c := &checkout{root: root, dir: filepath.Join(root, "repo"), home: filepath.Join(root, "home")}
	if err := os.MkdirAll(c.home, 0o700); err != nil {
		c.close()
		return nil, err
	}
	c.gitEnv = gitEnv(c.home, token)
	if _, err := runGit(ctx, root, c.gitEnv, "clone", "--quiet", "--depth", "50", "--single-branch",
		"--branch", branch, url, c.dir); err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}

// --- shared config --------------------------------------------------------------

type claudeCfg struct {
	codingCfg
	project, auth string
	authEnv       []string
	url, token    string
	model, budget string
	commands      []string
	timeout       time.Duration
}

func resolveClaude(n model.Node, run *model.Run, needToken bool, defTimeout time.Duration) (claudeCfg, func(string) string, *Result) {
	cfg, ex, fail := resolveCoding(n, run, needToken)
	if fail != nil {
		return claudeCfg{}, ex, fail
	}
	project := runProject(run)
	mode, authEnv, msg := claudeAuth(project)
	if msg != "" {
		r := failCode(msg)
		return claudeCfg{}, ex, &r
	}
	token, _ := githubToken(n, project)
	// Tier models are full model IDs, which the CLI accepts too. Without tiers (or
	// an explicit model on the step) the session uses claude_model.
	model := projVar(project, "claude_model")
	if cfg.pick.Tier != "" || cfg.pick.Reason == "set on this step" {
		model = cfg.pick.Model
	} else {
		cfg.pick.Model = model
	}
	usageFor(run).setPick(cfg.pick)
	c := claudeCfg{codingCfg: cfg, project: project, auth: mode, authEnv: authEnv,
		url: cloneURL(n, cfg.owner, cfg.repo), token: token,
		model: model, timeout: defTimeout}
	if b := projVar(project, "claude_max_budget_usd"); b != "" {
		if f, err := strconv.ParseFloat(b, 64); err == nil && f > 0 {
			c.budget = strconv.FormatFloat(f, 'f', 2, 64)
		}
	}
	if m, err := strconv.Atoi(projVar(project, "claude_timeout_minutes")); err == nil && m > 0 {
		c.timeout = time.Duration(m) * time.Minute
	}
	for _, p := range strings.FieldsFunc(projVar(project, "claude_allowed_commands"), func(r rune) bool { return r == ',' || r == '\n' || r == ';' }) {
		if p = strings.TrimSpace(p); p != "" && !strings.HasPrefix(p, "git push") {
			c.commands = append(c.commands, p)
		}
	}
	return c, ex, nil
}

// baseBranch is the node's explicit base, else the remote's default branch.
func (c claudeCfg) baseBranch(ctx context.Context, n model.Node, run *model.Run, env []string) (string, error) {
	if b := strings.TrimSpace(expandTemplate(run, secretExpand(c.project, n.Field("base")))); b != "" && !strings.Contains(b, "${") {
		return b, nil
	}
	return defaultBranch(ctx, env, c.url)
}

func (c claudeCfg) session(dir, home, prompt, system string, allowed, disallowed []string) claudeSpec {
	return claudeSpec{dir: dir, prompt: prompt, system: system, model: c.model, budget: c.budget,
		allowed: allowed, disallowed: disallowed, env: claudeEnv(c.auth, c.authEnv, home), timeout: c.timeout}
}

func (c claudeCfg) outputBase(o claudeOutcome) map[string]any {
	out := map[string]any{"engine": engineClaudeCode, "auth": c.auth, "repo": c.owner + "/" + c.repo,
		"turns": o.turns}
	if c.model != "" {
		out["model"] = c.model
	}
	if len(o.denials) > 0 {
		out["denied"] = o.denials
	}
	return out
}

func claudeFail(phase string, err error, o claudeOutcome) Result {
	out := map[string]any{"error": err.Error(), "engine": engineClaudeCode, "phase": phase}
	if len(o.denials) > 0 {
		out["denied"] = o.denials
		out["hint"] = "allow a command by adding its prefix to the claude_allowed_commands variable"
	}
	return Result{Outcome: model.StatusFailed, Output: out,
		Logs: []string{"claude_code: " + phase + ": " + err.Error()}}
}

var readOnlyTools = []string{"Read(./**)", "Glob", "Grep"}
var neverTools = []string{"WebFetch", "WebSearch", "Bash(git push:*)"}

// --- Plan changes ----------------------------------------------------------------

var planTagRe = regexp.MustCompile(`(?s)<change_plan>(.*?)</change_plan>`)

func claudePlan(ctx context.Context, n model.Node, run *model.Run) Result {
	cfg, ex, fail := resolveClaude(n, run, false, 10*time.Minute)
	if fail != nil {
		return *fail
	}
	task := ex("plan")
	if strings.TrimSpace(task) == "" || strings.Contains(task, "${memory.") {
		return failCode("no plan to work from (set plan, e.g. ${memory.plan})")
	}
	env := gitEnv(os.TempDir(), cfg.token)
	base, err := cfg.baseBranch(ctx, n, run, env)
	if err != nil {
		return claudeFail("clone", err, claudeOutcome{})
	}
	progress(ctx, fmt.Sprintf("Claude Code · cloning %s/%s (%s)", cfg.owner, cfg.repo, base))
	co, err := newCheckout(ctx, cfg.url, base, cfg.token)
	if err != nil {
		return claudeFail("clone", err, claudeOutcome{})
	}
	defer co.close()

	prompt := fmt.Sprintf(`Plan a code change in this repository. Do not modify anything.

Explore the code first (read files, search) so the plan names real files and fits
the existing structure, conventions and tests.

THE WORK:
%s

NOTES:
%s

Reply with:
1. A short summary of your approach (2–4 sentences).
2. The change plan: a JSON array inside <change_plan></change_plan> tags, listing
   every file to create or modify (include the tests to add or update), at most %d files:
   <change_plan>[{"path":"relative/path.ext","action":"create"|"modify","details":"what changes and why"}]</change_plan>
Use "create" only for files that do not exist yet. Paths are relative to the repository root.`,
		task, firstNonEmpty(ex("implementation"), "(none)"), cfg.maxFiles)
	prompt += reviewerFeedback(run, n.ID, "change_plan") + askBlock(n, run, "the change plan")
	system := contextBlock(cfg.project) + "You are a senior engineer planning a change for a human to approve. You may only read the repository."
	o, err := runClaude(ctx, cfg.session(co.dir, co.home, prompt, system,
		readOnlyTools, append([]string{"Edit", "Write", "NotebookEdit", "Bash"}, neverTools...)))
	cfg.recordClaude(o)
	if err != nil {
		return claudeFail("plan", err, o)
	}
	if !planTagRe.MatchString(o.result) {
		if q, ok := maybeQuestion(n, run, o.result); ok {
			return q
		}
	}
	planText := o.result
	if m := planTagRe.FindStringSubmatch(o.result); m != nil {
		planText = m[1]
	}
	items, perr := parseChangePlan(planText, cfg.maxFiles)
	if perr != "" {
		return claudeFail("plan", errors.New(perr), o)
	}
	approach := strings.TrimSpace(planTagRe.ReplaceAllString(o.result, ""))
	if len(approach) > 1500 {
		approach = approach[:1500] + "…"
	}
	formatted := formatChangePlan(items)
	shown := formatted
	if approach != "" {
		shown = "Approach: " + approach + "\n\n" + formatted
	}
	rawJSON, _ := json.Marshal(items)
	out := cfg.outputBase(o)
	out["change_plan"] = shown
	out["files"] = len(items)
	out["base"] = base
	return Result{Outcome: model.StatusSuccess, Output: out,
		MemoryWrites: map[string]any{"change_plan": shown, "change_plan_json": string(rawJSON), "base_branch": base},
		Logs:         []string{"claude_code: proposed changes —\n" + shown}}
}

// --- Apply changes ---------------------------------------------------------------

func claudeApply(ctx context.Context, n model.Node, run *model.Run) Result {
	cfg, ex, fail := resolveClaude(n, run, true, 20*time.Minute)
	if fail != nil {
		return *fail
	}
	changesJSON := firstNonEmpty(ex("changes"), ex("change_plan_json"))
	if strings.TrimSpace(changesJSON) == "" || strings.Contains(changesJSON, "${memory.") {
		return failCode("no approved change plan (set changes to ${memory.change_plan_json} from a coding_plan node)")
	}
	items, perr := parseChangePlan(changesJSON, cfg.maxFiles)
	if perr != "" {
		return failCode("could not read approved change plan: " + perr)
	}
	approved := map[string]bool{}
	for _, it := range items {
		approved[cleanRel(it.Path)] = true
	}

	env := gitEnv(os.TempDir(), cfg.token)
	base, err := cfg.baseBranch(ctx, n, run, env)
	if err != nil {
		return claudeFail("clone", err, claudeOutcome{})
	}
	branch := branchName(cfg.branchPrefix, cfg.ticket)
	feedback := ex("feedback")
	retry := feedbackBlock(feedback) != ""
	start := base
	if retry {
		start = branch // a CI-recovery retry continues from the pushed attempt
	}
	progress(ctx, fmt.Sprintf("Claude Code · cloning %s/%s (%s)", cfg.owner, cfg.repo, start))
	co, err := newCheckout(ctx, cfg.url, start, cfg.token)
	if err != nil && retry {
		start, retry = base, false
		co, err = newCheckout(ctx, cfg.url, base, cfg.token)
	}
	if err != nil {
		return claudeFail("clone", err, claudeOutcome{})
	}
	defer co.close()
	if _, err := runGit(ctx, co.dir, co.gitEnv, "checkout", "-q", "-B", branch); err != nil {
		return claudeFail("branch", err, claudeOutcome{})
	}

	paths := make([]string, 0, len(approved))
	for p := range approved {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var verify string
	if len(cfg.commands) > 0 {
		verify = "\nYou may run only these shell commands (and their arguments): " + strings.Join(cfg.commands, ", ") +
			".\nUse them to build and run the tests before you finish, and fix anything that fails."
	} else {
		verify = "\nShell commands are not available in this session; CI will run the tests on the pushed branch."
	}
	prompt := fmt.Sprintf(`Implement this approved change in the repository.

THE WORK:
%s

APPROVED CHANGE PLAN:
%s

You may create or modify ONLY these files (a person approved exactly this list):
%s
Do not create, modify, rename or delete any other file. Follow the repository's existing
conventions and any CLAUDE.md guidance.%s
Do not commit, push or change git history — Conduit commits and pushes for you.%s

Finish with a short summary of what you changed and how you checked it.`,
		ex("plan"), formatChangePlan(items), "- "+strings.Join(paths, "\n- "), verify, feedbackBlock(feedback))
	prompt += reviewerFeedback(run, n.ID, "diff")
	system := contextBlock(cfg.project) + "You are a senior engineer implementing a change a human has approved file by file."
	allowed := append(append([]string{}, readOnlyTools...), "Edit(./**)", "Write(./**)")
	for _, c := range cfg.commands {
		allowed = append(allowed, "Bash("+c+":*)")
	}
	o, err := runClaude(ctx, cfg.session(co.dir, co.home, prompt, system, allowed, neverTools))
	cfg.recordClaude(o)
	if err != nil {
		return claudeFail("apply", err, o)
	}

	changed, err := changedFiles(ctx, co)
	if err != nil {
		return claudeFail("diff", err, o)
	}
	if len(changed) == 0 {
		return claudeFail("apply", errors.New("Claude Code finished without changing any file"), o)
	}
	var extra []string
	for _, p := range changed {
		if !approved[p] {
			extra = append(extra, p)
		}
	}
	if len(extra) > 0 {
		r := claudeFail("apply", fmt.Errorf("changed files outside the approved list: %s — nothing was pushed", strings.Join(extra, ", ")), o)
		r.Output["unapproved_files"] = extra
		return r
	}

	msg := fmt.Sprintf("%s: %s", cfg.ticket, firstLine(firstNonEmpty(cfg.ticketSummary, "automated change")))
	if retry {
		msg += " (fix failing checks)"
	}

	// Review before push: hand the exact change to the Review Diff gate instead.
	if pushAfterReview(n) {
		cs, old, err := checkoutChangeSet(ctx, co, changed)
		if err != nil {
			return claudeFail("diff", err, o)
		}
		cs.Repo, cs.Base, cs.Branch, cs.Message, cs.Plan = cfg.owner+"/"+cfg.repo, start, branch, msg, formatChangePlan(items)
		out := cfg.outputBase(o)
		out["summary"] = strings.TrimSpace(o.result)
		r := prepared(cs, old, out, []string{"claude_code: " + strings.TrimSpace(o.result)})
		if r.MemoryWrites != nil {
			r.MemoryWrites["implementation_summary"] = strings.TrimSpace(o.result)
		}
		return r
	}

	if _, err := runGit(ctx, co.dir, co.gitEnv, append([]string{"add", "-A", "--"}, changed...)...); err != nil {
		return claudeFail("commit", err, o)
	}
	if _, err := runGit(ctx, co.dir, co.gitEnv, "-c", "user.name=Conduit Agent",
		"-c", "user.email=conduit-agent@users.noreply.github.com", "commit", "-q", "-m", msg); err != nil {
		return claudeFail("commit", err, o)
	}
	progress(ctx, "Claude Code · pushing "+branch)
	if _, err := runGit(ctx, co.dir, co.gitEnv, "push", "-q", "--force", "origin", "HEAD:refs/heads/"+branch); err != nil {
		return claudeFail("push", err, o)
	}
	sha, _ := runGit(ctx, co.dir, co.gitEnv, "rev-parse", "HEAD")
	sha = strings.TrimSpace(sha)
	stat, _ := runGit(ctx, co.dir, co.gitEnv, "show", "--stat", "--format=", "HEAD")
	diff, _ := runGit(ctx, co.dir, co.gitEnv, "show", "--format=", "HEAD")
	if len(diff) > 60000 {
		diff = diff[:60000] + "\n… (diff truncated)"
	}

	cfg.base = base
	fc := make([]fileChange, len(changed))
	for i, p := range changed {
		fc[i] = fileChange{Path: p}
	}
	res := cfg.applyResult(branch, sha, formatChangePlan(items), fc)
	for k, v := range cfg.outputBase(o) {
		res.Output[k] = v
	}
	res.Output["diff_stat"] = strings.TrimSpace(stat)
	res.Output["diff"] = diff
	res.Output["summary"] = strings.TrimSpace(o.result)
	res.MemoryWrites["implementation_summary"] = strings.TrimSpace(o.result)
	res.Logs = append(res.Logs, "claude_code: "+strings.TrimSpace(stat))
	return res
}

// checkoutChangeSet reads the changed files from the checkout into a change set
// (new content, create/modify/delete) on top of HEAD, with their HEAD contents
// for the review diff. Binary files are refused: the review and the GitHub push
// both work on text.
func checkoutChangeSet(ctx context.Context, co *checkout, changed []string) (changeSet, map[string]string, error) {
	var cs changeSet
	old := map[string]string{}
	head, err := runGit(ctx, co.dir, co.gitEnv, "rev-parse", "HEAD")
	if err != nil {
		return cs, nil, err
	}
	cs.BaseSHA = strings.TrimSpace(head)
	for _, p := range changed {
		prev, perr := runGit(ctx, co.dir, co.gitEnv, "show", "HEAD:"+p)
		inHead := perr == nil
		if inHead {
			old[p] = prev
		}
		b, rerr := os.ReadFile(filepath.Join(co.dir, filepath.FromSlash(p)))
		switch {
		case errors.Is(rerr, os.ErrNotExist):
			if inHead {
				cs.Files = append(cs.Files, fileEdit{Path: p, Action: "delete"})
			}
			continue
		case rerr != nil:
			return cs, nil, rerr
		case strings.IndexByte(string(b), 0) >= 0:
			return cs, nil, fmt.Errorf("%s is a binary file; review before push supports text files only (skip the Review Diff step to push it directly)", p)
		}
		action := "modify"
		if !inHead {
			action = "create"
		}
		cs.Files = append(cs.Files, fileEdit{Path: p, Action: action, Content: string(b)})
	}
	if len(cs.Files) == 0 {
		return cs, nil, errors.New("no file changes to review")
	}
	return cs, old, nil
}

// changedFiles lists the paths changed in the checkout's working tree, relative
// to the repo root. Claude Code's own scratch files (.claude/) are ignored.
func changedFiles(ctx context.Context, co *checkout) ([]string, error) {
	out, err := runGit(ctx, co.dir, co.gitEnv, "status", "--porcelain", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	var paths []string
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 {
			continue
		}
		status, p := f[:2], f[3:]
		if status[0] == 'R' || status[0] == 'C' {
			i++ // -z puts the original path of a rename in the next field
			if i < len(fields) && fields[i] != "" {
				paths = append(paths, cleanRel(fields[i]))
			}
		}
		p = cleanRel(p)
		if strings.HasPrefix(p, ".claude/") {
			continue
		}
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return dedupe(paths), nil
}

func cleanRel(p string) string {
	return strings.TrimPrefix(path.Clean(strings.TrimPrefix(strings.TrimSpace(p), "./")), "/")
}

func dedupe(s []string) []string {
	out := s[:0]
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}

// --- dispatch ----------------------------------------------------------------------

func execCodingPlanCtx(ctx context.Context, n model.Node, run *model.Run) Result {
	if codingEngine(n, runProject(run)) == engineClaudeCode {
		return claudePlan(ctx, n, run)
	}
	return execCodingPlan(n, run)
}

func execCodingApplyCtx(ctx context.Context, n model.Node, run *model.Run) Result {
	if codingEngine(n, runProject(run)) == engineClaudeCode {
		return claudeApply(ctx, n, run)
	}
	return codingApply(n, run, func(m string) { progress(ctx, m) })
}

// --- connection check ------------------------------------------------------------

// ClaudeStatus reports whether the Claude Code CLI is installed (and its version)
// and whether the operator allows server_login. It runs nothing billable.
func ClaudeStatus(ctx context.Context) map[string]any {
	res := map[string]any{"installed": false, "server_login_allowed": ServerLoginAllowed()}
	bin := claudeBin()
	if _, err := exec.LookPath(bin); err != nil {
		return res
	}
	res["installed"] = true
	vctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if v, err := exec.CommandContext(vctx, bin, "--version").Output(); err == nil {
		res["version"] = strings.TrimSpace(string(v))
	}
	return res
}

// CheckClaude verifies that Claude Code is installed and can sign in for a
// project: it runs one tiny prompt with the project's sign-in and reports the
// result (never the credential).
func CheckClaude(ctx context.Context, project string) map[string]any {
	res := map[string]any{"ok": false, "engine": codingEngine(model.Node{}, project), "server_login_allowed": ServerLoginAllowed()}
	bin := claudeBin()
	if _, err := exec.LookPath(bin); err != nil {
		res["error"] = fmt.Sprintf("Claude Code CLI %q is not installed on this server", bin)
		return res
	}
	if v, err := exec.CommandContext(ctx, bin, "--version").Output(); err == nil {
		res["version"] = strings.TrimSpace(string(v))
	}
	mode, authEnv, msg := claudeAuth(project)
	res["auth"] = mode
	if msg != "" {
		res["error"] = msg
		return res
	}
	root, err := os.MkdirTemp(os.Getenv("CONDUIT_WORK_DIR"), "conduit-claude-check-")
	if err != nil {
		res["error"] = err.Error()
		return res
	}
	defer os.RemoveAll(root)
	spec := claudeSpec{dir: root, prompt: "Reply with exactly: OK", model: firstNonEmpty(projVar(project, "claude_model"), "haiku"),
		disallowed: []string{"Bash", "Edit", "Write", "WebFetch", "WebSearch"},
		env:        claudeEnv(mode, authEnv, root), timeout: 90 * time.Second}
	o, err := runClaude(ctx, spec)
	if err != nil {
		res["error"] = err.Error()
		return res
	}
	res["ok"] = true
	res["reply"] = strings.TrimSpace(o.result)
	return res
}

// resultUsage reads token usage from a stream-json result event: the "usage"
// object, and the model with the largest output from "modelUsage" when present.
func resultUsage(ev map[string]any) (llmUsage, string) {
	num := func(m map[string]any, k string) int64 {
		f, _ := m[k].(float64)
		return int64(f)
	}
	var u llmUsage
	if m, ok := ev["usage"].(map[string]any); ok {
		u = llmUsage{input: num(m, "input_tokens"), output: num(m, "output_tokens"),
			cacheRead: num(m, "cache_read_input_tokens"), cacheWrite: num(m, "cache_creation_input_tokens")}
	}
	best, bestOut := "", int64(-1)
	if mu, ok := ev["modelUsage"].(map[string]any); ok {
		for name, v := range mu {
			m, _ := v.(map[string]any)
			if o := num(m, "outputTokens"); o > bestOut {
				best, bestOut = name, o
			}
		}
	}
	return u, best
}

// recordClaude adds a session's usage to the step. A subscription session is not
// billed per token, so its reported cost is kept as the API-price equivalent.
func (c claudeCfg) recordClaude(o claudeOutcome) {
	if o.usage == (llmUsage{}) && o.cost == 0 {
		return
	}
	c.usage.addReported(firstNonEmpty(o.model, c.model), o.usage, o.cost, c.auth == authSubscription || c.auth == authServerLogin)
}
