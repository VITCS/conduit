package components

import (
	"encoding/json"
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

func TestBranchName(t *testing.T) {
	if got := branchName("conduit/", "ENG 42: fix!"); got != "conduit/ENG-42-fix" {
		t.Errorf("branchName = %q", got)
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

	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Anthropic-shaped response carrying the JSON file array as text.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "text", "text": `[{"path":"README.md","content":"# hi"}]`}},
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
