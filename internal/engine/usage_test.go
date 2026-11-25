package engine

import (
	"math"
	"testing"
	"time"

	"conduit/internal/components"
	"conduit/internal/model"
)

// A step that loops (sent back once) is charged for both executions on the run,
// while its own record shows only the latest one.
func TestRunUsageSumsEveryExecution(t *testing.T) {
	components.Register("test_costly", func(n model.Node, run *model.Run) components.Result {
		return components.Result{Outcome: model.StatusSuccess, Output: map[string]any{},
			Usage: &model.Usage{Model: "claude-sonnet-5-5", Calls: 1, InputTokens: 1000, OutputTokens: 100, CostUSD: 0.003}}
	})
	e, st := newTestEngine(t)
	wf := sendBackWorkflow()
	wf.Nodes[1].Type = "test_costly"
	if err := st.PutWorkflow(wf); err != nil {
		t.Fatal(err)
	}
	run := e.Start(wf, "")
	get := func() model.Run { return runStatus(t, st, run.ID) }
	r := waitingAt(t, get, 0)
	e.ResolveWith(r.ID, "a1", Decision{Decision: DecisionSentBack, Feedback: "again"})
	r = waitingAt(t, get, 1)
	e.ResolveWith(r.ID, "a1", Decision{Decision: "approved"})
	waitFor(t, 3*time.Second, func() bool { return get().Status == model.StatusSuccess })

	r = get()
	if r.Usage == nil || r.Usage.Calls != 2 || r.Usage.InputTokens != 2000 || math.Abs(r.Usage.CostUSD-0.006) > 1e-9 {
		t.Fatalf("run usage = %+v", r.Usage)
	}
	if m := r.Usage.ByModel["claude-sonnet-5-5"]; m == nil || m.Calls != 2 {
		t.Fatalf("by model = %+v", r.Usage.ByModel)
	}
	if s := r.Steps["ag"].Usage; s == nil || s.Calls != 1 {
		t.Fatalf("step usage = %+v", s)
	}
}
