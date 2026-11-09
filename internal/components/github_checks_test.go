package components

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"conduit/internal/model"
)

func checksNode(fields map[string]string) model.Node {
	var cfg []model.Field
	for k, v := range fields {
		cfg = append(cfg, model.Field{Key: k, Value: v})
	}
	return model.Node{ID: "vchk", Type: "github_checks", Config: cfg}
}

func TestChecksMissingRef(t *testing.T) {
	r := execGitHubChecks(checksNode(map[string]string{"owner": "o", "repo": "r", "token": "t"}), &model.Run{})
	if r.Outcome != model.StatusFailed {
		t.Fatalf("want failed without ref, got %s", r.Outcome)
	}
}

func checksServer(t *testing.T, payload map[string]any) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(payload)
	}))
}

func TestChecksPass(t *testing.T) {
	srv := checksServer(t, map[string]any{"total_count": 2, "check_runs": []map[string]any{
		{"name": "build", "status": "completed", "conclusion": "success"},
		{"name": "test", "status": "completed", "conclusion": "success"},
	}})
	defer srv.Close()
	r := execGitHubChecks(checksNode(map[string]string{
		"owner": "o", "repo": "r", "api_base": srv.URL, "token": "t", "ref": "abc123",
	}), &model.Run{})
	if r.Outcome != model.StatusSuccess {
		t.Fatalf("want success, got %s (%v)", r.Outcome, r.Output)
	}
	if r.MemoryWrites["checks_pass"] != true {
		t.Errorf("checks_pass = %v", r.MemoryWrites["checks_pass"])
	}
}

func TestChecksFail(t *testing.T) {
	srv := checksServer(t, map[string]any{"total_count": 2, "check_runs": []map[string]any{
		{"name": "build", "status": "completed", "conclusion": "success"},
		{"name": "test", "status": "completed", "conclusion": "failure"},
	}})
	defer srv.Close()
	r := execGitHubChecks(checksNode(map[string]string{
		"owner": "o", "repo": "r", "api_base": srv.URL, "token": "t", "ref": "abc123",
	}), &model.Run{})
	if r.Outcome != model.StatusFailed {
		t.Fatalf("want failed, got %s", r.Outcome)
	}
}

func TestChecksNoneConfiguredPasses(t *testing.T) {
	srv := checksServer(t, map[string]any{"total_count": 0, "check_runs": []map[string]any{}})
	defer srv.Close()
	r := execGitHubChecks(checksNode(map[string]string{
		"owner": "o", "repo": "r", "api_base": srv.URL, "token": "t", "ref": "abc123",
	}), &model.Run{})
	if r.Outcome != model.StatusSuccess {
		t.Fatalf("want success when no checks configured, got %s", r.Outcome)
	}
}
