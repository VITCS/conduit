package components

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"conduit/internal/model"
)

func triageNode(fields map[string]string) model.Node {
	var cfg []model.Field
	for k, v := range fields {
		cfg = append(cfg, model.Field{Key: k, Value: v})
	}
	return model.Node{ID: "triage", Type: "triage", Config: cfg}
}

func TestTriageNoTicket(t *testing.T) {
	r := execTriage(triageNode(map[string]string{}), &model.Run{})
	if r.Outcome != model.StatusFailed {
		t.Fatalf("want failed with no ticket, got %s", r.Outcome)
	}
}

func TestTriageHeuristicReady(t *testing.T) {
	// No api_key → heuristic path. A rich, checkable description should pass.
	desc := "Users cannot log in via SSO. Expected: login should succeed. " +
		"Steps to reproduce: open /login, click SSO, see error. Acceptance: a test covers the SSO callback."
	r := execTriage(triageNode(map[string]string{
		"summary": "Fix SSO login callback error", "description": desc, "threshold": "60",
	}), &model.Run{})
	if r.Outcome != model.StatusSuccess {
		t.Fatalf("want success (ready), got %s (%v)", r.Outcome, r.Output)
	}
	if r.MemoryWrites["confidence_score"] == nil {
		t.Error("expected confidence_score in memory")
	}
}

func TestTriageHeuristicVague(t *testing.T) {
	r := execTriage(triageNode(map[string]string{
		"summary": "make it better", "description": "", "threshold": "60",
	}), &model.Run{})
	if r.Outcome != model.StatusFailed {
		t.Fatalf("want failed (needs clarification) for a vague ticket, got %s", r.Outcome)
	}
}

func TestTriageModelScore(t *testing.T) {
	// A stubbed anthropic-shaped response drives the score.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "text", "text": `{"score":90,"level":"high","reason":"clear and bounded","missing":[]}`}},
		})
	}))
	defer srv.Close()
	r := execTriage(triageNode(map[string]string{
		"summary": "x", "description": "y", "threshold": "60",
		"provider": "anthropic", "base_url": srv.URL, "api_key": "k", "model": "m",
	}), &model.Run{})
	if r.Outcome != model.StatusSuccess {
		t.Fatalf("want success, got %s (%v)", r.Outcome, r.Output)
	}
	if r.MemoryWrites["confidence"] != "high" {
		t.Errorf("confidence = %v", r.MemoryWrites["confidence"])
	}
}
