package components

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"conduit/internal/model"
)

var claudeTiers = map[string]string{
	"model_fast": "claude-haiku-5-5", "model_standard": "claude-sonnet-5-5", "model_max": "claude-opus-5-5",
	"llm_model": "claude-sonnet-4-6",
}

func mnode(id, typ string, fields ...string) model.Node {
	n := model.Node{ID: id, Type: typ}
	for i := 0; i+1 < len(fields); i += 2 {
		n.Config = append(n.Config, model.Field{Key: fields[i], Value: fields[i+1]})
	}
	return n
}

func TestPickModelWithoutTiersKeepsSharedModel(t *testing.T) {
	withProjectVars(t, map[string]string{"llm_model": "claude-sonnet-4-6"})
	p := pickModel(mnode("plan", "agent", "model", "${var:llm_model}"), &model.Run{ProjectID: "p"}, "claude-sonnet-4-6", true)
	if p.Model != "claude-sonnet-4-6" || p.Tier != "" || p.logLine() != "" {
		t.Fatalf("no tiers should change nothing, got %+v", p)
	}
}

func TestPickModelDefaultTiers(t *testing.T) {
	withProjectVars(t, claudeTiers)
	run := &model.Run{ProjectID: "p"}
	cases := []struct {
		n    model.Node
		want string
		tier string
	}{
		{mnode("triage", "triage", "model", "${var:llm_model}"), "claude-haiku-5-5", "fast"},
		{mnode("plan", "agent", "model", "${var:llm_model}"), "claude-sonnet-5-5", "standard"},
		{mnode("review", "agent", "model", "${var:llm_model}", "tier", "fast"), "claude-haiku-5-5", "fast"},
		{mnode("cplan", "coding_plan", "model", "${var:llm_model}", "tier", "best"), "claude-opus-5-5", "max"},
		{mnode("rev", "github_review"), "claude-sonnet-5-5", "standard"},
	}
	for _, c := range cases {
		p := pickModel(c.n, run, "fallback", true)
		if p.Model != c.want || p.Tier != c.tier {
			t.Errorf("%s: got %s/%s, want %s/%s", c.n.ID, p.Model, p.Tier, c.want, c.tier)
		}
	}
}

func TestPickModelExplicitModelWins(t *testing.T) {
	withProjectVars(t, claudeTiers)
	run := &model.Run{ProjectID: "p", Memory: map[string]any{"confidence_score": 20}}
	p := pickModel(mnode("plan", "coding_plan", "model", "claude-fable-5-2"), run, "claude-fable-5-2", true)
	if p.Model != "claude-fable-5-2" || p.Tier != "" || p.Reason != "set on this step" {
		t.Fatalf("explicit model should win, got %+v", p)
	}
}

func TestStepUpSignals(t *testing.T) {
	withProjectVars(t, claudeTiers)
	plan := func(n int) string {
		items := make([]map[string]string, n)
		for i := range items {
			items[i] = map[string]string{"path": "f" + string(rune('a'+i)) + ".go", "action": "modify", "details": "x"}
		}
		b, _ := json.Marshal(items)
		return string(b)
	}
	apply := mnode("apply", "coding_apply", "model", "${var:llm_model}")
	cases := []struct {
		name   string
		n      model.Node
		run    *model.Run
		model  string
		reason string
	}{
		{"confident small change", apply, &model.Run{Memory: map[string]any{"confidence_score": 90, "change_plan_json": plan(2)}}, "claude-sonnet-5-5", "default for this step"},
		{"low confidence", apply, &model.Run{Memory: map[string]any{"confidence_score": 60.0}}, "claude-opus-5-5", "triage confidence 60 is below 75"},
		{"large plan", apply, &model.Run{Memory: map[string]any{"confidence_score": 90, "change_plan_json": plan(7)}}, "claude-opus-5-5", "change plan touches 7 files (more than 5)"},
		{"ci retry", apply, &model.Run{Loops: map[string]int{"vchk\x00apply": 1}}, "claude-opus-5-5", "retry after a failed check"},
		{"sent back", mnode("plan", "agent", "model", "${var:llm_model}", "tier", "fast"),
			&model.Run{Loops: map[string]int{"appr\x00plan": 1}, Memory: map[string]any{"feedback_for_plan": "add tests"}}, "claude-sonnet-5-5", "sent back by a reviewer"},
		{"agents ignore confidence", mnode("impl", "agent", "model", "${var:llm_model}"), &model.Run{Memory: map[string]any{"confidence_score": 10}}, "claude-sonnet-5-5", "default for this step"},
		{"escalate=false", mnode("apply", "coding_apply", "escalate", "false"), &model.Run{Memory: map[string]any{"confidence_score": 10}}, "claude-sonnet-5-5", "default for this step"},
	}
	for _, c := range cases {
		c.run.ProjectID = "p"
		coding := strings.HasPrefix(c.n.Type, "coding")
		p := pickModel(c.n, c.run, "fallback", coding)
		if p.Model != c.model || p.Reason != c.reason {
			t.Errorf("%s: got %s (%s), want %s (%s)", c.name, p.Model, p.Reason, c.model, c.reason)
		}
	}
}

func TestStepUpCanBeTurnedOffAndThresholdsSet(t *testing.T) {
	vars := map[string]string{"escalate_confidence_below": "50", "escalate_files_over": "10"}
	for k, v := range claudeTiers {
		vars[k] = v
	}
	withProjectVars(t, vars)
	apply := mnode("apply", "coding_apply")
	if p := pickModel(apply, &model.Run{ProjectID: "p", Memory: map[string]any{"confidence_score": 60}}, "", true); p.Tier != "standard" {
		t.Errorf("60 is above a threshold of 50, got %+v", p)
	}
	vars["model_escalation"] = "off"
	if p := pickModel(apply, &model.Run{ProjectID: "p", Loops: map[string]int{"vchk\x00apply": 2}}, "", true); p.Tier != "standard" {
		t.Errorf("escalation off, got %+v", p)
	}
}

func TestMissingTierFallsBackToNearest(t *testing.T) {
	withProjectVars(t, map[string]string{"model_standard": "claude-sonnet-5-5"})
	p := pickModel(mnode("t", "triage"), &model.Run{ProjectID: "p"}, "x", false)
	if p.Model != "claude-sonnet-5-5" || p.Tier != "fast" {
		t.Fatalf("got %+v", p)
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestCallCost(t *testing.T) {
	withProjectVars(t, map[string]string{"model_prices": "gpt-4o-mini=0.15/0.60; my-model = 1/3"})
	cases := []struct {
		model string
		u     llmUsage
		want  float64
		ok    bool
	}{
		{"claude-sonnet-5-5", llmUsage{input: 1_000_000, output: 100_000}, 2 + 1, true},
		{"claude-opus-5-5", llmUsage{input: 10_000, output: 2_000}, 0.04 + 0.04, true},
		{"claude-opus-5-5", llmUsage{cacheRead: 1_000_000}, 0.20, true},
		{"claude-haiku-5-5", llmUsage{input: 50_000, output: 1_000}, 0.005 + 0.0005, true},
		{"claude-haiku-5-5", llmUsage{input: 200_000, output: 1_000}, 0.1 + 0.0025, true}, // long-prompt rate
		{"claude-opus-5-20260301", llmUsage{input: 1_000_000}, 5, true},                   // prefix match, not opus-5-5
		{"gpt-4o-mini", llmUsage{input: 1_000_000, output: 1_000_000}, 0.75, true},
		{"MY-MODEL", llmUsage{input: 1_000_000}, 1, true},
		{"llama-3", llmUsage{input: 10}, 0, false},
	}
	for _, c := range cases {
		got, ok := callCost("p", c.model, c.u)
		if ok != c.ok || !near(got, c.want) {
			t.Errorf("%s %+v: got %v/%v, want %v/%v", c.model, c.u, got, ok, c.want, c.ok)
		}
	}
}

// fakeAnthropic answers /v1/messages with a fixed reply and usage, recording the model asked for.
func fakeAnthropic(t *testing.T, reply string, in, out int64) (*httptest.Server, *[]string) {
	t.Helper()
	var models []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		models = append(models, body.Model)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content":     []map[string]string{{"type": "text", "text": reply}},
			"stop_reason": "end_turn",
			"usage":       map[string]int64{"input_tokens": in, "output_tokens": out},
		})
	}))
	t.Cleanup(srv.Close)
	old := AllowPrivateEgress
	AllowPrivateEgress = true
	t.Cleanup(func() { AllowPrivateEgress = old })
	return srv, &models
}

func TestExecRecordsUsageModelAndReason(t *testing.T) {
	srv, models := fakeAnthropic(t, "plan text", 12_000, 3_000)
	withProjectVars(t, claudeTiers)
	n := mnode("plan", "agent", "live", "true", "provider", "anthropic", "base_url", srv.URL,
		"api_key", "k", "model", "${var:llm_model}", "prompt", "Plan it")
	run := &model.Run{ProjectID: "p", Loops: map[string]int{"appr\x00plan": 1},
		Memory: map[string]any{"feedback_for_plan": "too vague"}}

	res := Exec(context.Background(), "agent", n, run)
	if res.Outcome != model.StatusSuccess {
		t.Fatalf("agent failed: %v", res.Output)
	}
	if (*models)[0] != "claude-opus-5-5" {
		t.Errorf("sent-back standard step should step up to the best model, asked for %q", (*models)[0])
	}
	u := res.Usage
	if u == nil || u.Calls != 1 || u.InputTokens != 12_000 || u.OutputTokens != 3_000 || u.Model != "claude-opus-5-5" {
		t.Fatalf("usage = %+v", u)
	}
	if want := 0.048 + 0.06; !near(u.CostUSD, want) {
		t.Errorf("cost = %v, want %v", u.CostUSD, want)
	}
	if res.Output["model_tier"] != "max" || res.Output["model_reason"] != "sent back by a reviewer" || res.Output["tokens_in"] != int64(12_000) {
		t.Errorf("output = %v", res.Output)
	}
	joined := strings.Join(res.Logs, "\n")
	for _, want := range []string{"model: using claude-opus-5-5 (max tier, sent back by a reviewer)", "usage: 1 call · 12,000 in / 3,000 out · $0.1080"} {
		if !strings.Contains(joined, want) {
			t.Errorf("logs missing %q:\n%s", want, joined)
		}
	}
	if _, ok := usageByRun.Load(run); ok {
		t.Error("collector not released after Exec")
	}
}

func TestExecWithoutModelCallsHasNoUsage(t *testing.T) {
	withProjectVars(t, claudeTiers)
	res := Exec(context.Background(), "agent", mnode("a", "agent", "prompt", "hi"), &model.Run{ProjectID: "p"}) // simulated
	if res.Usage != nil {
		t.Fatalf("simulated agent should report no usage, got %+v", res.Usage)
	}
}

func TestTriageUsesFastTier(t *testing.T) {
	srv, models := fakeAnthropic(t, `{"score":88,"level":"high","reason":"clear","missing":[]}`, 500, 40)
	withProjectVars(t, claudeTiers)
	n := mnode("triage", "triage", "provider", "anthropic", "base_url", srv.URL, "api_key", "k",
		"model", "${var:llm_model}", "summary", "Fix the login button", "description", "Expected: it works")
	res := Exec(context.Background(), "triage", n, &model.Run{ProjectID: "p"})
	if (*models)[0] != "claude-haiku-5-5" || res.Usage == nil || res.Output["model_tier"] != "fast" {
		t.Fatalf("model %v, usage %+v, output %v", *models, res.Usage, res.Output)
	}
}

func TestAddUsageKeepsPerModelBreakdown(t *testing.T) {
	var total *model.Usage
	total = AddUsage(total, &model.Usage{Model: "a", Calls: 1, InputTokens: 10, CostUSD: 0.5})
	total = AddUsage(total, &model.Usage{Model: "b", Calls: 2, OutputTokens: 5, SubscriptionUSD: 2})
	total = AddUsage(total, &model.Usage{Model: "a", Calls: 1, InputTokens: 5, CostUSD: 0.25})
	if total.Calls != 4 || total.InputTokens != 15 || !near(total.CostUSD, 0.75) || !near(total.SubscriptionUSD, 2) {
		t.Fatalf("total = %+v", total)
	}
	if a := total.ByModel["a"]; a.Calls != 2 || !near(a.CostUSD, 0.75) {
		t.Errorf("a = %+v", a)
	}
	if s := SortedModels(total.ByModel); s[0].Model != "b" {
		t.Errorf("most expensive first, got %s", s[0].Model)
	}
}

func TestClaudeResultUsage(t *testing.T) {
	var ev map[string]any
	_ = json.Unmarshal([]byte(`{"type":"result","total_cost_usd":0.42,
		"usage":{"input_tokens":1200,"output_tokens":800,"cache_read_input_tokens":50000,"cache_creation_input_tokens":3000},
		"modelUsage":{"claude-haiku-5-5":{"outputTokens":20},"claude-opus-5-5":{"outputTokens":780}}}`), &ev)
	u, m := resultUsage(ev)
	if u != (llmUsage{input: 1200, output: 800, cacheRead: 50000, cacheWrite: 3000}) || m != "claude-opus-5-5" {
		t.Fatalf("usage %+v model %q", u, m)
	}
	acc := &usageAcc{project: "p"}
	acc.addReported(m, u, 0.42, true)
	r := acc.result()
	if r.CostUSD != 0 || !near(r.SubscriptionUSD, 0.42) {
		t.Errorf("subscription work must not count as billed: %+v", r)
	}
	if s := usageSummary(r); !strings.Contains(s, "subscription, not billed") {
		t.Errorf("summary = %q", s)
	}
}
