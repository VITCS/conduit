package engine

import (
	"testing"
	"time"

	"conduit/internal/model"
	"conduit/internal/seed"
	"conduit/internal/store"
)

// newTestEngine returns an engine with zero pacing so runs complete instantly.
func newTestEngine(t *testing.T) (*Engine, store.Store) {
	t.Helper()
	st := store.NewMem()
	if err := st.PutWorkflow(seed.Workflow()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	e := New(st)
	e.StepDelay = 0
	e.TriggerDelay = 0
	return e, st
}

// waitFor polls cond until it is true or the deadline passes.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

func runStatus(t *testing.T, st store.Store, id string) model.Run {
	t.Helper()
	r, ok, err := st.GetRun(id)
	if err != nil || !ok {
		t.Fatalf("get run %s: ok=%v err=%v", id, ok, err)
	}
	return r
}

func TestRunReachesApprovalGate(t *testing.T) {
	e, st := newTestEngine(t)
	run := e.Start(seed.Workflow(), "")

	waitFor(t, 2*time.Second, func() bool {
		return runStatus(t, st, run.ID).Status == model.StatusWaiting
	})

	r := runStatus(t, st, run.ID)
	// Everything up to the gate should have succeeded; the gate waits.
	for _, id := range []string{"t1", "g1", "p1", "c1", "h1", "m1"} {
		if got := r.Steps[id].Status; got == model.StatusPending || got == model.StatusFailed {
			t.Errorf("step %s = %s, want completed before gate", id, got)
		}
	}
	if r.Steps["a1"].Status != model.StatusWaiting {
		t.Errorf("approval step = %s, want waiting", r.Steps["a1"].Status)
	}
	// Downstream of the gate must not have run yet.
	if s := r.Steps["u1"].Status; s != model.StatusPending {
		t.Errorf("post-approval step u1 = %s, want pending while waiting", s)
	}
}

func TestApproveCompletesRun(t *testing.T) {
	e, st := newTestEngine(t)
	run := e.Start(seed.Workflow(), "")

	waitFor(t, 2*time.Second, func() bool {
		return runStatus(t, st, run.ID).Status == model.StatusWaiting
	})
	if !e.Resolve(run.ID, "a1", "approved") {
		t.Fatal("Resolve(approved) returned false")
	}
	waitFor(t, 2*time.Second, func() bool {
		return runStatus(t, st, run.ID).Status == model.StatusSuccess
	})

	r := runStatus(t, st, run.ID)
	if r.Steps["a1"].Status != model.StatusSuccess {
		t.Errorf("a1 = %s, want success", r.Steps["a1"].Status)
	}
	if r.Steps["u1"].Status != model.StatusSuccess {
		t.Errorf("u1 = %s, want success", r.Steps["u1"].Status)
	}
	// The failure-branch status node was never reached -> skipped.
	if r.Steps["sf"].Status != model.StatusSkipped {
		t.Errorf("sf = %s, want skipped", r.Steps["sf"].Status)
	}
	if _, ok := r.Memory["preview-envs/154"]; !ok {
		t.Errorf("memory missing preview-envs/154, got keys %v", keys(r.Memory))
	}
	if r.FinishedAt == nil {
		t.Error("FinishedAt not set on completed run")
	}
}

func TestRejectFailsRun(t *testing.T) {
	e, st := newTestEngine(t)
	run := e.Start(seed.Workflow(), "")

	waitFor(t, 2*time.Second, func() bool {
		return runStatus(t, st, run.ID).Status == model.StatusWaiting
	})
	if !e.Resolve(run.ID, "a1", "rejected") {
		t.Fatal("Resolve(rejected) returned false")
	}
	waitFor(t, 2*time.Second, func() bool {
		return runStatus(t, st, run.ID).Status == model.StatusFailed
	})

	r := runStatus(t, st, run.ID)
	if r.Steps["a1"].Status != model.StatusFailed {
		t.Errorf("a1 = %s, want failed", r.Steps["a1"].Status)
	}
	if r.Steps["u1"].Status != model.StatusSkipped {
		t.Errorf("u1 = %s, want skipped after reject", r.Steps["u1"].Status)
	}
}

func TestResolveUnknownGateIsFalse(t *testing.T) {
	e, _ := newTestEngine(t)
	if e.Resolve("run_does_not_exist", "a1", "approved") {
		t.Error("Resolve on unknown run returned true")
	}
}

// TestFailureBranch verifies a failing HTTP node takes the "failed" edge and
// leaves the success branch skipped.
func TestFailureBranch(t *testing.T) {
	st := store.NewMem()
	wf := model.Workflow{
		ID:   "wf_fail",
		Name: "Failing health check",
		Nodes: []model.Node{
			{ID: "t", Type: "github", Kind: model.KindTrigger, Title: "Trigger"},
			{ID: "h", Type: "http", Kind: model.KindStep, Title: "Health",
				Config: []model.Field{{Key: "url", Value: "http://svc/fail"}}},
			{ID: "ok", Type: "status", Kind: model.KindStatus, Title: "Mark OK"},
			{ID: "bad", Type: "status", Kind: model.KindStatus, Title: "Mark Failed"},
		},
		Edges: []model.Edge{
			{From: "t", To: "h"},
			{From: "h", To: "ok", Label: "success"},
			{From: "h", To: "bad", Label: "failed"},
		},
	}
	if err := st.PutWorkflow(wf); err != nil {
		t.Fatal(err)
	}
	e := New(st)
	e.StepDelay, e.TriggerDelay = 0, 0
	run := e.Start(wf, "")

	waitFor(t, 2*time.Second, func() bool {
		return runStatus(t, st, run.ID).Status == model.StatusFailed
	})
	r := runStatus(t, st, run.ID)
	if r.Steps["h"].Status != model.StatusFailed {
		t.Errorf("h = %s, want failed", r.Steps["h"].Status)
	}
	if r.Steps["bad"].Status != model.StatusCreated {
		t.Errorf("bad = %s, want created (failed branch taken)", r.Steps["bad"].Status)
	}
	if r.Steps["ok"].Status != model.StatusSkipped {
		t.Errorf("ok = %s, want skipped (success branch not taken)", r.Steps["ok"].Status)
	}
}

func TestEdgeMatches(t *testing.T) {
	cases := []struct {
		label   string
		outcome model.Status
		want    bool
	}{
		{"", model.StatusSuccess, true},
		{"success", model.StatusSuccess, true},
		{"approved", model.StatusSuccess, true},
		{"failed", model.StatusSuccess, false},
		{"failed", model.StatusFailed, true},
		{"success", model.StatusFailed, false},
		{"", model.StatusFailed, false},
		{"success", model.StatusCreated, true},
	}
	for _, c := range cases {
		if got := edgeMatches(c.label, c.outcome); got != c.want {
			t.Errorf("edgeMatches(%q,%s) = %v, want %v", c.label, c.outcome, got, c.want)
		}
	}
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
