package engine

import (
	"testing"
	"time"

	"conduit/internal/model"
)

// sendBackWorkflow: start → agent → approval ─approved→ done
//
//	├─failed→ stop
//	└─sent_back (max 2)→ agent
func sendBackWorkflow() model.Workflow {
	return model.Workflow{ID: "wf_sb", ProjectID: model.DefaultProjectID, Name: "send back",
		Nodes: []model.Node{
			{ID: "t1", Type: "start", Kind: model.KindTrigger, Title: "Start"},
			{ID: "ag", Type: "agent", Kind: model.KindStep, Title: "Plan",
				Config: []model.Field{{Key: "prompt", Value: "Plan it"}}},
			{ID: "a1", Type: "approval", Kind: model.KindStep, Title: "Approve plan",
				Config: []model.Field{{Key: "approvers", Value: "you"}}},
			{ID: "done", Type: "noop", Kind: model.KindStep, Title: "Done"},
			{ID: "stop", Type: "status", Kind: model.KindStatus, Title: "Needs attention"},
		},
		Edges: []model.Edge{
			{From: "t1", To: "ag"},
			{From: "ag", To: "a1", Label: "success"},
			{From: "a1", To: "done", Label: "approved"},
			{From: "a1", To: "stop", Label: "failed"},
			{From: "a1", To: "ag", Label: "sent_back", MaxAttempts: 2},
		}}
}

func startSendBack(t *testing.T) (*Engine, func() model.Run) {
	t.Helper()
	e, st := newTestEngine(t)
	wf := sendBackWorkflow()
	if err := st.PutWorkflow(wf); err != nil {
		t.Fatal(err)
	}
	run := e.Start(wf, "")
	get := func() model.Run { return runStatus(t, st, run.ID) }
	return e, get
}

func waitingAt(t *testing.T, get func() model.Run, loops int) model.Run {
	t.Helper()
	waitFor(t, 3*time.Second, func() bool {
		r := get()
		return r.Status == model.StatusWaiting && r.Steps["a1"] != nil &&
			r.Steps["a1"].Status == model.StatusWaiting && r.Loops["a1\x00ag"] == loops
	})
	return get()
}

func TestSendBackLoopsWithFeedbackThenApproves(t *testing.T) {
	e, get := startSendBack(t)
	r := waitingAt(t, get, 0)
	if r.Steps["a1"].Output["send_back"] != true || num(r.Steps["a1"].Output["send_backs_left"]) != 2 {
		t.Fatalf("gate must offer send back with 2 left: %v", r.Steps["a1"].Output)
	}

	if !e.ResolveWith(r.ID, "a1", Decision{Decision: DecisionSentBack, Feedback: "Use a table, not a list"}) {
		t.Fatal("send back not delivered")
	}
	r = waitingAt(t, get, 1)
	if r.Memory["feedback_for_ag"] != "Use a table, not a list" || r.Memory["review_feedback"] != "Use a table, not a list" {
		t.Fatalf("feedback not recorded for the agent: %v", r.Memory)
	}
	if h, _ := r.Memory["feedback_history"].([]any); len(h) != 1 {
		t.Fatalf("feedback_history = %v", r.Memory["feedback_history"])
	}
	if num(r.Steps["a1"].Output["send_backs_left"]) != 1 {
		t.Fatalf("send_backs_left after one = %v", r.Steps["a1"].Output["send_backs_left"])
	}
	if r.Steps["done"] != nil && r.Steps["done"].Status == model.StatusSuccess {
		t.Fatal("downstream ran before approval")
	}

	e.Resolve(r.ID, "a1", "approved")
	waitFor(t, 3*time.Second, func() bool { return get().Status == model.StatusSuccess })
	if s := get().Steps["done"].Status; s != model.StatusSuccess {
		t.Fatalf("done = %s, want success", s)
	}
}

func TestSendBackLimitCountsAsRejected(t *testing.T) {
	e, get := startSendBack(t)
	for i := 0; i < 2; i++ {
		r := waitingAt(t, get, i)
		e.ResolveWith(r.ID, "a1", Decision{Decision: DecisionSentBack, Feedback: "again"})
	}
	r := waitingAt(t, get, 2)
	if _, ok := r.Steps["a1"].Output["send_back"]; ok {
		t.Fatalf("no send-backs left: the gate must stop offering it: %v", r.Steps["a1"].Output)
	}
	e.ResolveWith(r.ID, "a1", Decision{Decision: DecisionSentBack, Feedback: "one more"})
	waitFor(t, 3*time.Second, func() bool { return get().Status == model.StatusFailed })
	r = get()
	if r.Steps["a1"].Status != model.StatusFailed || r.Steps["a1"].Output["reason"] != "send-back limit reached" {
		t.Fatalf("a1 = %s %v", r.Steps["a1"].Status, r.Steps["a1"].Output)
	}
	if s := r.Steps["stop"].Status; s == model.StatusPending || s == model.StatusSkipped {
		t.Fatalf("failure path not taken: stop = %s", r.Steps["stop"].Status)
	}
}

// num reads a number from a step output (int in memory, float64 after a JSON round trip).
func num(v any) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case float64:
		return n
	}
	return -1
}
