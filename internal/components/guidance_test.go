package components

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"conduit/internal/model"
)

// captureLLM is a fake "custom" model endpoint that records every prompt.
func captureLLM(t *testing.T, reply string) (*httptest.Server, *[]string) {
	t.Helper()
	var prompts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Prompt string `json:"prompt"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		prompts = append(prompts, body.Prompt)
		_ = json.NewEncoder(w).Encode(map[string]string{"text": reply})
	}))
	t.Cleanup(srv.Close)
	old := AllowPrivateEgress
	AllowPrivateEgress = true
	t.Cleanup(func() { AllowPrivateEgress = old })
	return srv, &prompts
}

func withProjectVars(t *testing.T, vars map[string]string) {
	t.Helper()
	old := ResolveVar
	ResolveVar = func(_, n string) (string, bool) { v, ok := vars[n]; return v, ok }
	t.Cleanup(func() { ResolveVar = old })
}

func TestAgentGetsProjectContextAndReviewerFeedback(t *testing.T) {
	srv, prompts := captureLLM(t, "revised plan")
	withProjectVars(t, map[string]string{"project_context": "Stack: Next.js. Tests: npm test. Never touch billing/."})
	n := model.Node{ID: "plan", Type: "agent", Config: []model.Field{
		{Key: "live", Value: "true"}, {Key: "provider", Value: "custom"},
		{Key: "base_url", Value: srv.URL}, {Key: "prompt", Value: "Plan ticket ENG-9"}}}
	run := &model.Run{ProjectID: "p",
		Memory: map[string]any{"feedback_for_plan": "Split the migration into its own step"},
		Steps:  map[string]*model.StepRun{"plan": {Output: map[string]any{"text": "1. Do everything at once"}}}}

	res := execAgent(n, run)
	if res.Outcome != model.StatusSuccess {
		t.Fatalf("agent failed: %v", res.Output)
	}
	p := (*prompts)[0]
	for _, want := range []string{
		"PROJECT CONTEXT", "Never touch billing/", "Plan ticket ENG-9",
		"SENT YOUR PREVIOUS PROPOSAL BACK", "Split the migration into its own step",
		"YOUR PREVIOUS PROPOSAL", "1. Do everything at once",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q:\n%s", want, p)
		}
	}
	if strings.Index(p, "PROJECT CONTEXT") > strings.Index(p, "Plan ticket ENG-9") {
		t.Error("project context should come before the task")
	}
}

func TestAgentPromptUnchangedWithoutContextOrFeedback(t *testing.T) {
	srv, prompts := captureLLM(t, "plan")
	withProjectVars(t, map[string]string{})
	n := model.Node{ID: "plan", Type: "agent", Config: []model.Field{
		{Key: "live", Value: "true"}, {Key: "provider", Value: "custom"},
		{Key: "base_url", Value: srv.URL}, {Key: "prompt", Value: "Plan ticket ENG-9"}, {Key: "ask", Value: "false"}}}
	execAgent(n, &model.Run{ProjectID: "p"})
	if got := (*prompts)[0]; got != "Plan ticket ENG-9" {
		t.Fatalf("prompt changed with nothing to add: %q", got)
	}
}

func TestCodingPlanAPIGetsContextAndFeedback(t *testing.T) {
	srv, prompts := captureLLM(t, `[{"path":"a.go","action":"modify","details":"x"}]`)
	withProjectVars(t, map[string]string{"project_context": "Go 1.24; keep handlers thin."})
	n := model.Node{ID: "cplan", Type: "coding_plan", Config: []model.Field{
		{Key: "owner", Value: "acme"}, {Key: "repo", Value: "web"},
		{Key: "provider", Value: "custom"}, {Key: "base_url", Value: srv.URL},
		{Key: "plan", Value: "Add a health endpoint"}}}
	run := &model.Run{ProjectID: "p", Memory: map[string]any{"feedback_for_cplan": "Also add a test file"}}
	res := execCodingPlan(n, run)
	if res.Outcome != model.StatusSuccess {
		t.Fatalf("plan failed: %v", res.Output)
	}
	p := (*prompts)[0]
	for _, want := range []string{"keep handlers thin", "Add a health endpoint", "Also add a test file"} {
		if !strings.Contains(p, want) {
			t.Errorf("coding plan prompt missing %q:\n%s", want, p)
		}
	}
}
