package components

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"conduit/internal/model"
)

func jiraNode(fields map[string]string) model.Node {
	var cfg []model.Field
	for k, v := range fields {
		cfg = append(cfg, model.Field{Key: k, Value: v})
	}
	return model.Node{ID: "src", Type: "jira", Config: cfg}
}

func TestJiraMissingConfig(t *testing.T) {
	r := execJira(jiraNode(map[string]string{"base_url": "https://x.atlassian.net"}), &model.Run{})
	if r.Outcome != model.StatusFailed {
		t.Fatalf("want failed without email/token, got %s", r.Outcome)
	}
}

func TestJiraFetchByJQL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !strings.HasPrefix(req.Header.Get("Authorization"), "Basic ") {
			w.WriteHeader(401)
			return
		}
		if !strings.Contains(req.URL.Path, "/rest/api/3/search") {
			w.WriteHeader(404)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issues": []map[string]any{{
				"key": "ENG-42",
				"fields": map[string]any{
					"summary": "Add rate limiting",
					"status":  map[string]any{"name": "Ready for Dev"},
					"description": map[string]any{
						"type": "doc", "content": []any{
							map[string]any{"type": "paragraph", "content": []any{
								map[string]any{"type": "text", "text": "Throttle the public API."},
							}},
						},
					},
				},
			}},
		})
	}))
	defer srv.Close()

	r := execJira(jiraNode(map[string]string{
		"base_url": srv.URL, "email": "me@co.com", "api_token": "tok",
		"jql": "status = 'Ready for Dev'",
	}), &model.Run{})
	if r.Outcome != model.StatusSuccess {
		t.Fatalf("want success, got %s (%v)", r.Outcome, r.Output)
	}
	if r.MemoryWrites["ticket_key"] != "ENG-42" {
		t.Errorf("ticket_key = %v", r.MemoryWrites["ticket_key"])
	}
	if got := r.MemoryWrites["ticket_description"]; got != "Throttle the public API." {
		t.Errorf("description = %q", got)
	}
	if r.MemoryWrites["ticket_source"] != "jira" {
		t.Errorf("source = %v", r.MemoryWrites["ticket_source"])
	}
}

func TestJiraFetchNoneFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"issues": []any{}})
	}))
	defer srv.Close()
	r := execJira(jiraNode(map[string]string{"base_url": srv.URL, "email": "a@b.c", "api_token": "t"}), &model.Run{})
	if r.Outcome != model.StatusFailed {
		t.Fatalf("want failed when no issues, got %s", r.Outcome)
	}
}

func TestJiraComment(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost || !strings.Contains(req.URL.Path, "/comment") {
			w.WriteHeader(404)
			return
		}
		_ = json.NewDecoder(req.Body).Decode(&gotBody)
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "10001"})
	}))
	defer srv.Close()

	r := execJira(jiraNode(map[string]string{
		"action": "comment", "base_url": srv.URL, "email": "a@b.c", "api_token": "t",
		"issue_key": "ENG-42", "body": "line one\nline two",
	}), &model.Run{})
	if r.Outcome != model.StatusSuccess {
		t.Fatalf("want success, got %s (%v)", r.Outcome, r.Output)
	}
	if gotBody["body"] == nil {
		t.Fatalf("expected ADF body, got %v", gotBody)
	}
}

func TestADFToText(t *testing.T) {
	raw := []byte(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Hello"}]},{"type":"paragraph","content":[{"type":"text","text":"World"}]}]}`)
	if got := adfToText(raw); got != "Hello\nWorld" {
		t.Errorf("adfToText = %q", got)
	}
}
