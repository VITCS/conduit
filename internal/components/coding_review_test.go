package components

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"conduit/internal/model"
)

func TestUnifiedDiff(t *testing.T) {
	old := "a\nb\nc\nd\ne\nf\ng\nh\n"
	cur := "a\nb\nc\nD\ne\nf\ng\nh\ni\n"
	d, add, del := unifiedDiff("x.txt", old, cur, "modify")
	if add != 2 || del != 1 {
		t.Fatalf("+%d -%d, want +2 -1\n%s", add, del, d)
	}
	for _, want := range []string{"diff --git a/x.txt b/x.txt", "--- a/x.txt", "+++ b/x.txt", "@@ -1,8 +1,9 @@", "-d\n+D\n", "+i\n"} {
		if !strings.Contains(d, want) {
			t.Errorf("diff missing %q:\n%s", want, d)
		}
	}
	d, add, _ = unifiedDiff("new.go", "", "package x\n", "create")
	if add != 1 || !strings.Contains(d, "new file mode") || !strings.Contains(d, "--- /dev/null") {
		t.Fatalf("create diff:\n%s", d)
	}
	d, _, del = unifiedDiff("gone.txt", "bye\n", "", "delete")
	if del != 1 || !strings.Contains(d, "deleted file mode") || !strings.Contains(d, "+++ /dev/null") {
		t.Fatalf("delete diff:\n%s", d)
	}
	// Two distant edits become two hunks.
	long := strings.Repeat("line\n", 40)
	edited := "first\n" + strings.Repeat("line\n", 38) + "last\n"
	d, _, _ = unifiedDiff("l.txt", "x\n"+long[5:]+"y\n", edited, "modify")
	if strings.Count(d, "@@ -") != 2 {
		t.Fatalf("want 2 hunks:\n%s", d)
	}
}

// fakeGitHub records the Git Data API calls a push makes.
type fakeGitHub struct {
	mu      sync.Mutex
	entries []map[string]any
	parent  string
	ref     string
	srv     *httptest.Server
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	g := &fakeGitHub{}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		p := r.URL.Path
		switch {
		case r.Method == "GET" && strings.Contains(p, "/git/ref/heads/"):
			json.NewEncoder(w).Encode(map[string]any{"object": map[string]any{"sha": "basesha"}})
		case r.Method == "GET" && strings.Contains(p, "/git/commits/"):
			json.NewEncoder(w).Encode(map[string]any{"tree": map[string]any{"sha": "basetree"}})
		case r.Method == "POST" && strings.HasSuffix(p, "/git/blobs"):
			json.NewEncoder(w).Encode(map[string]any{"sha": "blob-" + body["content"].(string)[:3]})
		case r.Method == "POST" && strings.HasSuffix(p, "/git/trees"):
			for _, e := range body["tree"].([]any) {
				g.entries = append(g.entries, e.(map[string]any))
			}
			json.NewEncoder(w).Encode(map[string]any{"sha": "newtree"})
		case r.Method == "POST" && strings.HasSuffix(p, "/git/commits"):
			g.parent = body["parents"].([]any)[0].(string)
			json.NewEncoder(w).Encode(map[string]any{"sha": "newcommit123"})
		case strings.Contains(p, "/git/refs"):
			g.ref, _ = body["ref"].(string)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{})
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(g.srv.Close)
	old := AllowPrivateEgress
	AllowPrivateEgress = true
	t.Cleanup(func() { AllowPrivateEgress = old })
	return g
}

func TestCodingPushCommitsTheReviewedChangeSet(t *testing.T) {
	gh := newFakeGitHub(t)
	cs := changeSet{Repo: "acme/web", Base: "main", BaseSHA: "base123", Branch: "conduit/ENG-1", Message: "ENG-1: thing",
		Files: []fileEdit{{Path: "a.txt", Action: "modify", Content: "new a"}, {Path: "old.txt", Action: "delete"}}}
	raw, _ := json.Marshal(cs)
	n := model.Node{ID: "push", Type: "coding_push", Config: []model.Field{
		{Key: "owner", Value: "acme"}, {Key: "repo", Value: "web"}, {Key: "token", Value: "t"}, {Key: "api_base", Value: gh.srv.URL}}}
	res := execCodingPush(n, &model.Run{ProjectID: "p", Memory: map[string]any{memChangeSet: string(raw)}})
	if res.Outcome != model.StatusSuccess {
		t.Fatalf("push failed: %v", res.Output)
	}
	if gh.parent != "base123" {
		t.Errorf("commit parent = %q, want the reviewed base commit", gh.parent)
	}
	if len(gh.entries) != 2 || gh.entries[1]["path"] != "old.txt" || gh.entries[1]["sha"] != nil {
		t.Errorf("tree entries = %v (deletion must have a null sha)", gh.entries)
	}
	if res.MemoryWrites["work_branch"] != "conduit/ENG-1" || res.MemoryWrites["commit_sha"] != "newcommit123" {
		t.Errorf("memory = %v", res.MemoryWrites)
	}

	// A change set for another repo is refused.
	cs.Repo = "acme/other"
	raw, _ = json.Marshal(cs)
	res = execCodingPush(n, &model.Run{ProjectID: "p", Memory: map[string]any{memChangeSet: string(raw)}})
	if res.Outcome != model.StatusFailed {
		t.Fatal("pushing a change set reviewed for another repo must fail")
	}
}

func TestClaudeApplyReviewModeDoesNotPush(t *testing.T) {
	f := newClaudeFixture(t)
	f.mode(t, "apply")
	n := applyNode(f)
	n.Config = append(n.Config, model.Field{Key: "push", Value: "after review"})
	res := Exec(context.Background(), "coding_apply", n, &model.Run{ProjectID: "p"})
	if res.Outcome != model.StatusSuccess {
		t.Fatalf("prepare failed: %v", res.Output)
	}
	if out, err := exec.Command("git", "--git-dir", f.bare, "rev-parse", "--verify", "conduit/ENG-1").CombinedOutput(); err == nil {
		t.Fatalf("review mode must not push anything: %s", out)
	}
	var cs changeSet
	if err := json.Unmarshal([]byte(res.MemoryWrites[memChangeSet].(string)), &cs); err != nil {
		t.Fatal(err)
	}
	if cs.Repo != "acme/web" || cs.Branch != "conduit/ENG-1" || len(cs.BaseSHA) < 7 || len(cs.Files) != 2 {
		t.Fatalf("change set = %+v", cs)
	}
	for _, fe := range cs.Files {
		if fe.Path == "app.txt" && (fe.Action != "modify" || !strings.Contains(fe.Content, "hello from claude")) {
			t.Errorf("app.txt edit = %+v", fe)
		}
		if fe.Path == "app_test.txt" && fe.Action != "create" {
			t.Errorf("app_test.txt action = %q, want create", fe.Action)
		}
	}
	d, _ := res.MemoryWrites[memChangeDiff].(string)
	if !strings.Contains(d, "+hello from claude") || !strings.Contains(d, "new file mode") {
		t.Fatalf("review diff:\n%s", d)
	}
}

func TestAPIApplyReviewModeDoesNotPush(t *testing.T) {
	gh := newFakeGitHub(t)
	llm, _ := captureLLM(t, "package main\n")
	withProjectVars(t, map[string]string{})
	n := model.Node{ID: "apply", Type: "coding_apply", Config: []model.Field{
		{Key: "owner", Value: "acme"}, {Key: "repo", Value: "web"}, {Key: "token", Value: "t"}, {Key: "api_base", Value: gh.srv.URL},
		{Key: "provider", Value: "custom"}, {Key: "base_url", Value: llm.URL}, {Key: "ticket", Value: "ENG-2"},
		{Key: "changes", Value: `[{"path":"main.go","action":"create","details":"x"}]`}, {Key: "push", Value: "false"}}}
	res := execCodingApply(n, &model.Run{ProjectID: "p"})
	if res.Outcome != model.StatusSuccess {
		t.Fatalf("prepare failed: %v", res.Output)
	}
	if gh.ref != "" || len(gh.entries) != 0 {
		t.Fatalf("review mode pushed: ref=%q entries=%v", gh.ref, gh.entries)
	}
	if !strings.Contains(res.MemoryWrites[memChangeDiff].(string), "+package main") {
		t.Fatalf("diff = %v", res.MemoryWrites[memChangeDiff])
	}
}

// TestUnifiedDiffAppliesWithGit checks the generated diffs are real patches: for
// random edits, `git apply` of our diff must turn the old file into the new one.
func TestUnifiedDiffAppliesWithGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	rng := uint32(7)
	rand := func(n int) int { rng = rng*1664525 + 1013904223; return int(rng>>8) % n }
	words := []string{"alpha", "beta", "gamma", "delta", "", "}", "return x", "if y {"}
	for iter := 0; iter < 200; iter++ {
		var old []string
		for i := 0; i < 5+rand(60); i++ {
			old = append(old, words[rand(len(words))])
		}
		cur := append([]string(nil), old...)
		for e := 0; e < 1+rand(6); e++ {
			p := rand(len(cur) + 1)
			switch rand(3) {
			case 0:
				cur = append(cur[:p], append([]string{"new " + words[rand(len(words))]}, cur[p:]...)...)
			case 1:
				if p < len(cur) {
					cur = append(cur[:p], cur[p+1:]...)
				}
			default:
				if p < len(cur) {
					cur[p] = "changed " + cur[p]
				}
			}
		}
		oldS, curS := strings.Join(old, "\n")+"\n", strings.Join(cur, "\n")+"\n"
		if oldS == curS {
			continue
		}
		dir := t.TempDir()
		writeFile(t, dir+"/f.txt", oldS)
		d, _, _ := unifiedDiff("f.txt", oldS, curS, "modify")
		writeFile(t, dir+"/p.diff", d)
		cmd := exec.Command("git", "apply", "p.diff")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("iter %d: git apply failed: %v %s\n--- diff ---\n%s", iter, err, out, d)
		}
		if got := readFile(t, dir+"/f.txt"); got != curS {
			t.Fatalf("iter %d: applying the diff gave the wrong file\n--- diff ---\n%s", iter, d)
		}
	}
}

func writeFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
