package components

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"conduit/internal/model"
)

func TestAgentSimulatedByDefault(t *testing.T) {
	res := For("agent")(node("agent",
		model.Field{Key: "provider", Value: "anthropic"},
		model.Field{Key: "prompt", Value: "Refine the issue"},
	), &model.Run{})
	if res.Outcome != model.StatusSuccess {
		t.Fatalf("agent outcome = %s, want success", res.Outcome)
	}
	if res.Output["simulated"] != true {
		t.Error("default agent should be simulated")
	}
	if _, ok := res.Output["text"].(string); !ok {
		t.Error("agent output missing text")
	}
}

func TestAgentLiveAnthropic(t *testing.T) {
	var gotPath, gotKey, gotVersion, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"a concrete plan"}]}`))
	}))
	defer srv.Close()

	res := For("agent")(node("agent",
		model.Field{Key: "live", Value: "true"},
		model.Field{Key: "provider", Value: "anthropic"},
		model.Field{Key: "base_url", Value: srv.URL},
		model.Field{Key: "api_key", Value: "sk-test"},
		model.Field{Key: "model", Value: "claude-x"},
		model.Field{Key: "prompt", Value: "Refine"},
	), &model.Run{})
	if res.Outcome != model.StatusSuccess {
		t.Fatalf("live agent outcome = %s", res.Outcome)
	}
	if res.Output["text"] != "a concrete plan" {
		t.Errorf("text = %v, want 'a concrete plan'", res.Output["text"])
	}
	if gotPath != "/v1/messages" || gotKey != "sk-test" || gotVersion == "" {
		t.Errorf("request shape wrong: path=%s key=%s version=%s", gotPath, gotKey, gotVersion)
	}
	if !strings.Contains(gotBody, `"claude-x"`) {
		t.Errorf("model not in body: %s", gotBody)
	}
}

func TestAgentLiveOpenAI(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/chat/completions" {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"openai says hi"}}]}`))
	}))
	defer srv.Close()

	res := For("agent")(node("agent",
		model.Field{Key: "live", Value: "true"},
		model.Field{Key: "provider", Value: "openai"},
		model.Field{Key: "base_url", Value: srv.URL},
		model.Field{Key: "api_key", Value: "k"},
		model.Field{Key: "prompt", Value: "hi"},
	), &model.Run{})
	if res.Outcome != model.StatusSuccess || res.Output["text"] != "openai says hi" {
		t.Fatalf("openai agent: outcome=%s text=%v", res.Outcome, res.Output["text"])
	}
	if gotAuth != "Bearer k" {
		t.Errorf("auth header = %q, want Bearer k", gotAuth)
	}
}

func TestAgentLiveFailureStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	res := For("agent")(node("agent",
		model.Field{Key: "live", Value: "true"}, model.Field{Key: "provider", Value: "anthropic"},
		model.Field{Key: "base_url", Value: srv.URL}, model.Field{Key: "api_key", Value: "k"},
		model.Field{Key: "prompt", Value: "x"},
	), &model.Run{})
	if res.Outcome != model.StatusFailed {
		t.Errorf("500 from provider should fail the step, got %s", res.Outcome)
	}
}

func TestExpandTemplate(t *testing.T) {
	run := &model.Run{
		Input:  map[string]any{"issue": "fix login bug"},
		Memory: map[string]any{"pr_url": "https://x/pr/1"},
	}
	got := expandTemplate(run, "Issue: ${input.issue}; PR: ${memory.pr_url}; keep ${input.missing}")
	want := "Issue: fix login bug; PR: https://x/pr/1; keep ${input.missing}"
	if got != want {
		t.Errorf("expandTemplate = %q, want %q", got, want)
	}
}

// Regression: provider and model must resolve ${var:…} project variables, not be
// sent to the API verbatim (which produced `anthropic 404: model: ${var:llm_model}`).
func TestAgentLiveResolvesProjectVars(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
	}))
	defer srv.Close()

	prevV, prevS := ResolveVar, ResolveSecret
	ResolveVar = func(projectID, name string) (string, bool) {
		if projectID == "ai-factory" && name == "llm_model" {
			return "claude-opus-4-8", true
		}
		if projectID == "ai-factory" && name == "llm_provider" {
			return "anthropic", true
		}
		return "", false
	}
	ResolveSecret = func(projectID, name string) (string, bool) {
		if name == "llm_key" {
			return "sk-live", true
		}
		return "", false
	}
	defer func() { ResolveVar, ResolveSecret = prevV, prevS }()

	res := For("agent")(node("agent",
		model.Field{Key: "live", Value: "true"},
		model.Field{Key: "provider", Value: "${var:llm_provider}"},
		model.Field{Key: "model", Value: "${var:llm_model}"},
		model.Field{Key: "api_key", Value: "${secret:llm_key}"},
		model.Field{Key: "base_url", Value: srv.URL},
		model.Field{Key: "prompt", Value: "Refine"},
	), &model.Run{ProjectID: "ai-factory"})
	if res.Outcome != model.StatusSuccess {
		t.Fatalf("outcome = %s (%v)", res.Outcome, res.Output["error"])
	}
	if !strings.Contains(gotBody, `"claude-opus-4-8"`) {
		t.Errorf("resolved model not in request body: %s", gotBody)
	}
	if strings.Contains(gotBody, "${var:") {
		t.Errorf("unresolved var sent to API: %s", gotBody)
	}
}

// An unresolved ${var:…} must fail fast with guidance, not hit the provider.
func TestAgentLiveUnresolvedVarFailsClearly(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"x"}]}`))
	}))
	defer srv.Close()

	prevV := ResolveVar
	ResolveVar = func(_, _ string) (string, bool) { return "", false } // nothing set
	defer func() { ResolveVar = prevV }()

	res := For("agent")(node("agent",
		model.Field{Key: "live", Value: "true"},
		model.Field{Key: "provider", Value: "anthropic"},
		model.Field{Key: "model", Value: "${var:llm_model}"},
		model.Field{Key: "base_url", Value: srv.URL},
		model.Field{Key: "prompt", Value: "Refine"},
	), &model.Run{ProjectID: "ai-factory"})
	if res.Outcome != model.StatusFailed {
		t.Fatalf("outcome = %s, want failed", res.Outcome)
	}
	if called {
		t.Error("provider was called despite unresolved config")
	}
	if msg, _ := res.Output["error"].(string); !strings.Contains(msg, "llm_model") {
		t.Errorf("error should name the missing variable, got: %v", res.Output["error"])
	}
}
