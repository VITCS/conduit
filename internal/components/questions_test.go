package components

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"conduit/internal/model"
)

func TestParseQuestion(t *testing.T) {
	q, opts, ok := parseQuestion("Thinking…\n<question> Which login should the page use? </question>\n<option>Email</option><option> Google </option><option></option>")
	if !ok || q != "Which login should the page use?" || len(opts) != 2 || opts[1] != "Google" {
		t.Fatalf("got %q %q %v", q, opts, ok)
	}
	if _, _, ok := parseQuestion("[{\"path\":\"a.go\"}]"); ok {
		t.Error("a normal answer is not a question")
	}
	if _, _, ok := parseQuestion("<question>  </question>"); ok {
		t.Error("an empty question is not a question")
	}
}

func TestQuestionsLeftAndAskBlock(t *testing.T) {
	withProjectVars(t, map[string]string{})
	n := mnode("plan", "agent")
	run := &model.Run{ProjectID: "p", Memory: map[string]any{}}
	if questionsLeft(n, run) != 2 {
		t.Fatalf("default should allow 2 questions")
	}
	if b := askBlock(n, run, "the plan"); !strings.Contains(b, "<question>") || !strings.Contains(b, "Instead of the plan") || !strings.Contains(b, "2 more question") {
		t.Errorf("ask block: %s", b)
	}
	run.Memory["answers_for_plan"] = []any{
		map[string]any{"question": "Q1?", "answer": "A1"},
		map[string]any{"question": "Q2?", "answer": "A2"},
	}
	b := askBlock(n, run, "the plan")
	if questionsLeft(n, run) != 0 || strings.Contains(b, "<question>") {
		t.Errorf("no questions should be left: %s", b)
	}
	if !strings.Contains(b, "Q: Q1?\nA: A1") || !strings.Contains(b, "do not ask these again") {
		t.Errorf("answers missing: %s", b)
	}
	if questionsLeft(mnode("plan", "agent", "ask", "false"), &model.Run{ProjectID: "p"}) != 0 {
		t.Error("ask=false turns asking off")
	}
	withProjectVars(t, map[string]string{"agent_questions": "off"})
	if questionsLeft(n, &model.Run{ProjectID: "p"}) != 0 {
		t.Error("agent_questions=off turns asking off")
	}
}

// replyLLM is a fake custom endpoint that answers each call with the next reply.
func replyLLM(t *testing.T, replies ...string) (*httptest.Server, *[]string) {
	t.Helper()
	var prompts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Prompt string `json:"prompt"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		prompts = append(prompts, body.Prompt)
		reply := replies[min(len(prompts)-1, len(replies)-1)]
		_ = json.NewEncoder(w).Encode(map[string]string{"text": reply})
	}))
	t.Cleanup(srv.Close)
	old := AllowPrivateEgress
	AllowPrivateEgress = true
	t.Cleanup(func() { AllowPrivateEgress = old })
	return srv, &prompts
}

func TestLiveAgentAsksThenUsesTheAnswer(t *testing.T) {
	srv, prompts := replyLLM(t,
		"<question>Postgres or SQLite?</question><option>Postgres</option><option>SQLite</option>",
		"Plan: use Postgres.")
	withProjectVars(t, map[string]string{})
	n := mnode("plan", "agent", "live", "true", "provider", "custom", "base_url", srv.URL, "prompt", "Plan storage", "output", "plan")
	run := &model.Run{ProjectID: "p", Memory: map[string]any{}}

	res := Exec(context.Background(), "agent", n, run)
	if res.Outcome != OutcomeQuestion || res.Output["question"] != "Postgres or SQLite?" || len(res.Output["options"].([]string)) != 2 {
		t.Fatalf("expected a question, got %v %v", res.Outcome, res.Output)
	}
	if res.MemoryWrites != nil {
		t.Error("a question must not write the step's output to memory")
	}

	run.Memory["answers_for_plan"] = []any{map[string]any{"question": "Postgres or SQLite?", "answer": "Postgres"}}
	res = Exec(context.Background(), "agent", n, run)
	if res.Outcome != model.StatusSuccess || res.MemoryWrites["plan"] != "Plan: use Postgres." {
		t.Fatalf("expected the plan, got %v %v", res.Outcome, res.Output)
	}
	if p := (*prompts)[1]; !strings.Contains(p, "Q: Postgres or SQLite?\nA: Postgres") {
		t.Errorf("second prompt lacks the answer:\n%s", p)
	}
}

func TestAgentCannotAskWhenOutOfQuestions(t *testing.T) {
	srv, _ := replyLLM(t, "<question>Again?</question>")
	withProjectVars(t, map[string]string{})
	n := mnode("plan", "agent", "live", "true", "provider", "custom", "base_url", srv.URL, "prompt", "x", "max_questions", "0")
	res := Exec(context.Background(), "agent", n, &model.Run{ProjectID: "p"})
	if res.Outcome != model.StatusSuccess {
		t.Fatalf("with no questions left the reply is the answer, got %v", res.Outcome)
	}
}

func TestCodingPlanAsks(t *testing.T) {
	srv, prompts := replyLLM(t, "<question>Which page gets the banner?</question><option>Home</option>")
	withProjectVars(t, map[string]string{"owner": "acme", "repo": "web"})
	n := mnode("cplan", "coding_plan", "owner", "${var:owner}", "repo", "${var:repo}", "provider", "custom",
		"base_url", srv.URL, "api_key", "k", "plan", "Add a banner")
	res := execCodingPlan(n, &model.Run{ProjectID: "p", Memory: map[string]any{}})
	if res.Outcome != OutcomeQuestion || res.Output["question"] != "Which page gets the banner?" {
		t.Fatalf("got %v %v", res.Outcome, res.Output)
	}
	if !strings.Contains((*prompts)[0], "Instead of the JSON array") {
		t.Error("plan prompt should explain how to ask")
	}
}
