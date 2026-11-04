package components

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"conduit/internal/model"
)

func codeNode(fields map[string]string) model.Node {
	var cfg []model.Field
	for k, v := range fields {
		cfg = append(cfg, model.Field{Key: k, Value: v})
	}
	return model.Node{ID: "code", Type: "coding_agent", Config: cfg}
}

func TestCodingAgentMissingRepo(t *testing.T) {
	r := execCodingAgent(codeNode(map[string]string{"token": "t", "plan": "do it"}), &model.Run{})
	if r.Outcome != model.StatusFailed {
		t.Fatalf("want failed without owner/repo, got %s", r.Outcome)
	}
}

func TestCodingAgentNoPlan(t *testing.T) {
	r := execCodingAgent(codeNode(map[string]string{"owner": "o", "repo": "r", "token": "t"}), &model.Run{})
	if r.Outcome != model.StatusFailed {
		t.Fatalf("want failed without plan, got %s", r.Outcome)
	}
}

func TestParseFileChanges(t *testing.T) {
	// Tolerates prose + fences around the array, drops unsafe paths.
	text := "Sure! ```json\n[{\"path\":\"a.txt\",\"content\":\"hi\"},{\"path\":\"../evil\",\"content\":\"x\"}]\n``` done"
	files, errMsg := parseFileChanges(text, 10)
	if errMsg != "" {
		t.Fatalf("unexpected err: %s", errMsg)
	}
	if len(files) != 1 || files[0].Path != "a.txt" {
		t.Fatalf("got %+v", files)
	}
}

func TestParseChangePlan(t *testing.T) {
	text := "Plan: ```json\n[{\"path\":\"a.go\",\"action\":\"modify\",\"details\":\"add fn\"},{\"path\":\"b.go\",\"action\":\"create\",\"details\":\"new\"},{\"path\":\"../x\",\"action\":\"create\"}]\n```"
	items, errMsg := parseChangePlan(text, 10)
	if errMsg != "" {
		t.Fatalf("unexpected err: %s", errMsg)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 safe items, got %d: %+v", len(items), items)
	}
	got := formatChangePlan(items)
	if !strings.Contains(got, "[modify] a.go — add fn") || !strings.Contains(got, "[create] b.go") {
		t.Errorf("formatChangePlan = %q", got)
	}
}

func TestBranchName(t *testing.T) {
	if got := branchName("conduit/", "ENG 42: fix!"); got != "conduit/ENG-42-fix" {
		t.Errorf("branchName = %q", got)
	}
}

// stubGitData returns an httptest server that answers the GitHub Git Data API
// calls the coding agent makes (ref -> commit -> blobs -> tree -> commit -> ref),
// plus a 404 for any Contents API lookup (so "modify" downgrades to "create").
func stubGitData() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.Contains(req.URL.Path, "/contents/"):
			w.WriteHeader(404)
		case strings.HasSuffix(req.URL.Path, "/git/ref/heads/main"):
			_ = json.NewEncoder(w).Encode(map[string]any{"object": map[string]any{"sha": "base-sha"}})
		case strings.Contains(req.URL.Path, "/git/commits/base-sha"):
			_ = json.NewEncoder(w).Encode(map[string]any{"tree": map[string]any{"sha": "base-tree"}})
		case strings.HasSuffix(req.URL.Path, "/git/blobs"):
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": "blob-sha"})
		case strings.HasSuffix(req.URL.Path, "/git/trees"):
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": "tree-sha"})
		case strings.HasSuffix(req.URL.Path, "/git/commits"):
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": "commit-sha"})
		case strings.HasSuffix(req.URL.Path, "/git/refs"):
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]any{"ref": "refs/heads/conduit/ENG-7"})
		default:
			w.WriteHeader(404)
		}
	}))
}

func TestCodingPlanWritesMemory(t *testing.T) {
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "text", "text": `[{"path":"a.go","action":"modify","details":"add fn"}]`}},
		})
	}))
	defer llm.Close()

	r := execCodingPlan(codeNode(map[string]string{
		"owner": "o", "repo": "r",
		"provider": "anthropic", "base_url": llm.URL, "api_key": "k", "model": "m",
		"plan": "add a function",
	}), &model.Run{})
	if r.Outcome != model.StatusSuccess {
		t.Fatalf("want success, got %s (%v)", r.Outcome, r.Output)
	}
	j, _ := r.MemoryWrites["change_plan_json"].(string)
	items, errMsg := parseChangePlan(j, 10)
	if errMsg != "" || len(items) != 1 || items[0].Path != "a.go" {
		t.Fatalf("change_plan_json not usable: %q (%s)", j, errMsg)
	}
}

func TestCodingApplyFromApprovedPlan(t *testing.T) {
	gh := stubGitData()
	defer gh.Close()
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "text", "text": `[{"path":"README.md","content":"# hi"}]`}},
		})
	}))
	defer llm.Close()

	r := execCodingApply(codeNode(map[string]string{
		"owner": "o", "repo": "r", "base": "main",
		"api_base": gh.URL, "token": "ghtok",
		"provider": "anthropic", "base_url": llm.URL, "api_key": "k", "model": "m",
		"changes": `[{"path":"README.md","action":"create","details":"new"}]`,
		"plan":    "add a readme", "ticket": "ENG-7",
	}), &model.Run{})
	if r.Outcome != model.StatusSuccess {
		t.Fatalf("want success, got %s (%v)", r.Outcome, r.Output)
	}
	if r.MemoryWrites["work_branch"] != "conduit/ENG-7" {
		t.Errorf("work_branch = %v", r.MemoryWrites["work_branch"])
	}
}

func TestCodingApplyNeedsPlan(t *testing.T) {
	r := execCodingApply(codeNode(map[string]string{"owner": "o", "repo": "r", "token": "t"}), &model.Run{})
	if r.Outcome != model.StatusFailed {
		t.Fatalf("want failed without changes, got %s", r.Outcome)
	}
}

// Full happy path: a live LLM (stubbed) + the GitHub Git Data API (stubbed) end
// with work_branch written to memory and a PR-ready branch created.
func TestCodingAgentPushesBranch(t *testing.T) {
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/git/ref/heads/main"):
			_ = json.NewEncoder(w).Encode(map[string]any{"object": map[string]any{"sha": "base-sha"}})
		case strings.Contains(req.URL.Path, "/git/commits/base-sha"):
			_ = json.NewEncoder(w).Encode(map[string]any{"tree": map[string]any{"sha": "base-tree"}})
		case strings.HasSuffix(req.URL.Path, "/git/blobs"):
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": "blob-sha"})
		case strings.HasSuffix(req.URL.Path, "/git/trees"):
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": "tree-sha"})
		case strings.HasSuffix(req.URL.Path, "/git/commits"):
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": "commit-sha"})
		case strings.HasSuffix(req.URL.Path, "/git/refs"):
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]any{"ref": "refs/heads/conduit/ENG-7"})
		default:
			w.WriteHeader(404)
		}
	}))
	defer gh.Close()

	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		// The plan prompt asks for a CHANGE PLAN; the implement prompt doesn't.
		var text string
		if strings.Contains(string(b), "CHANGE PLAN for the work") {
			text = `[{"path":"README.md","action":"create","details":"add a readme"}]`
		} else {
			text = `[{"path":"README.md","content":"# hi"}]`
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
		})
	}))
	defer llm.Close()

	r := execCodingAgent(codeNode(map[string]string{
		"owner": "o", "repo": "r", "base": "main",
		"api_base": gh.URL, "token": "ghtok",
		"provider": "anthropic", "base_url": llm.URL, "api_key": "llmkey", "model": "m",
		"plan": "Add a readme", "ticket": "ENG-7",
	}), &model.Run{})

	if r.Outcome != model.StatusSuccess {
		t.Fatalf("want success, got %s (%v)", r.Outcome, r.Output)
	}
	if r.MemoryWrites["work_branch"] != "conduit/ENG-7" {
		t.Errorf("work_branch = %v", r.MemoryWrites["work_branch"])
	}
}
