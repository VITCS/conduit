package engine

import (
	"testing"
	"time"

	"conduit/internal/model"
)

// start → agent that asks (simulated) → done
func questionWorkflow() model.Workflow {
	return model.Workflow{ID: "wf_q", ProjectID: model.DefaultProjectID, Name: "question",
		Nodes: []model.Node{
			{ID: "t1", Type: "start", Kind: model.KindTrigger, Title: "Start"},
			{ID: "ag", Type: "agent", Kind: model.KindStep, Title: "Plan",
				Config: []model.Field{{Key: "prompt", Value: "Plan it"}, {Key: "output", Value: "plan"},
					{Key: "question", Value: "Which database?"}, {Key: "question_options", Value: "Postgres | SQLite"}}},
			{ID: "done", Type: "noop", Kind: model.KindStep, Title: "Done"},
		},
		Edges: []model.Edge{{From: "t1", To: "ag"}, {From: "ag", To: "done", Label: "success"}}}
}

func startQuestion(t *testing.T) (*Engine, func() model.Run) {
	t.Helper()
	e, st := newTestEngine(t)
	wf := questionWorkflow()
	if err := st.PutWorkflow(wf); err != nil {
		t.Fatal(err)
	}
	run := e.Start(wf, "")
	get := func() model.Run { return runStatus(t, st, run.ID) }
	waitFor(t, 3*time.Second, func() bool {
		r := get()
		return r.Status == model.StatusWaiting && r.Steps["ag"].Status == model.StatusWaiting
	})
	return e, get
}

func TestStepQuestionPausesThenContinuesWithTheAnswer(t *testing.T) {
	e, get := startQuestion(t)
	r := get()
	out := r.Steps["ag"].Output
	if out["kind"] != "question" || out["question"] != "Which database?" {
		t.Fatalf("waiting output = %v", out)
	}
	if opts, _ := out["options"].([]any); len(opts) != 2 || opts[0] != "Postgres" {
		t.Fatalf("options = %#v", out["options"])
	}
	if !e.ResolveWith(r.ID, "ag", Decision{Decision: DecisionAnswered, Feedback: "Postgres"}) {
		t.Fatal("answer not delivered")
	}
	waitFor(t, 3*time.Second, func() bool { return get().Status == model.StatusSuccess })
	r = get()
	qa, _ := r.Memory["answers_for_ag"].([]any)
	if len(qa) != 1 || qa[0].(map[string]any)["answer"] != "Postgres" {
		t.Fatalf("answers = %#v", r.Memory["answers_for_ag"])
	}
	if r.Steps["ag"].Status != model.StatusSuccess || r.Steps["done"].Status != model.StatusSuccess {
		t.Fatalf("steps: ag=%s done=%s", r.Steps["ag"].Status, r.Steps["done"].Status)
	}
	if r.Memory["plan"] == nil {
		t.Error("the step's output should be written once it answers")
	}
}

func TestStoppingInsteadOfAnsweringFailsTheStep(t *testing.T) {
	e, get := startQuestion(t)
	e.ResolveWith(get().ID, "ag", Decision{Decision: "rejected"})
	waitFor(t, 3*time.Second, func() bool { return get().Status == model.StatusFailed })
	if s := get().Steps["ag"]; s.Status != model.StatusFailed || s.Output["decision"] != "stopped" {
		t.Fatalf("step = %+v", s)
	}
}
