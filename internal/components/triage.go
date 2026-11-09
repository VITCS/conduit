// triage is the intake confidence gate. Before a ticket is implemented, it scores
// how ready the work is for an autonomous agent — clear outcome, bounded scope, a
// checkable success condition — and returns success only when the score clears a
// threshold. Low-confidence or ambiguous work routes out (to a "Needs
// clarification" status) so a human improves the ticket first, instead of the
// factory turning a vague request into a confident-looking wrong change. This is
// Superplane's "confidence is a routing signal, not a promise."
package components

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"conduit/internal/model"
)

var jsonObjRe = regexp.MustCompile(`(?s)\{.*\}`)

// execTriage scores the ticket in memory and passes/fails on a threshold.
// Config: provider, model, api_key (LLM); summary (${memory.ticket_summary}),
// description (${memory.ticket_description}); threshold (0-100, default 60).
func execTriage(n model.Node, run *model.Run) Result {
	project := runProject(run)
	ex := func(f string) string { return expandTemplate(run, secretExpand(project, n.Field(f))) }

	summary := strings.TrimSpace(ex("summary"))
	desc := strings.TrimSpace(ex("description"))
	if (summary == "" || strings.Contains(summary, "${memory.")) && (desc == "" || strings.Contains(desc, "${memory.")) {
		return failTriage("no ticket to score (set summary/description, e.g. ${memory.ticket_summary})", "", 0)
	}
	threshold := 60
	if v, e := strconv.Atoi(n.Field("threshold")); e == nil && v >= 0 && v <= 100 {
		threshold = v
	}

	score, level, reason, missing := scoreTicket(n, ex, project, summary, desc)
	mem := map[string]any{
		"confidence":         level,
		"confidence_score":   score,
		"confidence_reason":  reason,
		"confidence_missing": strings.Join(missing, "; "),
	}
	out := map[string]any{"score": score, "level": level, "reason": reason, "missing": missing, "threshold": threshold}

	if score >= threshold {
		return Result{Outcome: model.StatusSuccess, Output: out, MemoryWrites: mem,
			Logs: []string{fmt.Sprintf("triage: ready (score %d ≥ %d) — %s", score, threshold, reason)}}
	}
	out["blocked_reason"] = reason
	return Result{Outcome: model.StatusFailed, Output: out, MemoryWrites: mem,
		Logs: []string{fmt.Sprintf("triage: needs clarification (score %d < %d) — %s", score, threshold, reason)}}
}

// scoreTicket asks the model for a structured readiness verdict, falling back to a
// conservative heuristic when no live model is configured or the call fails.
func scoreTicket(n model.Node, ex func(string) string, project, summary, desc string) (score int, level, reason string, missing []string) {
	apiKey := secretExpand(project, firstNonEmpty(n.Field("api_key"), n.Field("key")))
	if strings.TrimSpace(apiKey) == "" || strings.Contains(apiKey, "${secret:") {
		return heuristicScore(summary, desc)
	}
	provider := strings.ToLower(firstNonEmpty(ex("provider"), "anthropic"))
	prompt := fmt.Sprintf(`You are a triage classifier for an autonomous coding factory.
Decide whether this ticket is well-scoped enough to implement automatically.
Output ONLY JSON, no prose:
{"score":0-100,"level":"high|medium|low","reason":"one sentence","missing":["what is unclear"]}
Score HIGH when: a clear outcome, a bounded surface area, and a checkable success condition.
Score LOW when: ambiguous, needs a product decision, or has no acceptance criteria.

TICKET:
%s

%s`, summary, desc)

	text, status, err := callLLM(provider, ex("base_url"), apiKey, ex("model"), prompt, 400)
	if err != nil || status < 200 || status >= 300 {
		s, l, r, m := heuristicScore(summary, desc)
		return s, l, r + " (model unavailable; heuristic)", m
	}
	var v struct {
		Score   int      `json:"score"`
		Level   string   `json:"level"`
		Reason  string   `json:"reason"`
		Missing []string `json:"missing"`
	}
	if m := jsonObjRe.FindString(text); m != "" {
		_ = json.Unmarshal([]byte(m), &v)
	}
	if v.Reason == "" && v.Score == 0 { // couldn't parse → heuristic
		return heuristicScore(summary, desc)
	}
	if v.Score < 0 {
		v.Score = 0
	}
	if v.Score > 100 {
		v.Score = 100
	}
	if v.Level == "" {
		v.Level = levelFor(v.Score)
	}
	return v.Score, v.Level, firstNonEmpty(v.Reason, "scored by model"), v.Missing
}

var readySignals = []string{"expected", "should", "acceptance", "steps to reproduce", "reproduce", "given", "when ", "then ", "error", "stack trace", "api", "endpoint", "test"}

// heuristicScore is a deterministic, conservative fallback used offline.
func heuristicScore(summary, desc string) (int, string, string, []string) {
	score := 35
	var missing []string
	if len(desc) >= 120 {
		score += 25
	} else {
		missing = append(missing, "a fuller description of the expected behaviour")
	}
	low := strings.ToLower(summary + " " + desc)
	hits := 0
	for _, s := range readySignals {
		if strings.Contains(low, s) {
			hits++
		}
	}
	score += min(hits*8, 30)
	if hits == 0 {
		missing = append(missing, "a checkable success condition (tests / acceptance criteria)")
	}
	if len(strings.Fields(summary)) >= 4 {
		score += 10
	}
	if score > 100 {
		score = 100
	}
	return score, levelFor(score), "heuristic readiness estimate", missing
}

// HeuristicConfidence scores a ticket's readiness offline (no model). Exported for
// intake-time routing: the webhook can set a confidence before a run exists.
func HeuristicConfidence(summary, description string) (score int, reason string) {
	s, _, r, _ := heuristicScore(summary, description)
	return s, r
}

func levelFor(score int) string {
	switch {
	case score >= 75:
		return "high"
	case score >= 50:
		return "medium"
	default:
		return "low"
	}
}

func failTriage(msg, level string, score int) Result {
	return Result{Outcome: model.StatusFailed,
		Output: map[string]any{"error": msg, "score": score, "level": level},
		Logs:   []string{"triage: " + msg}}
}
