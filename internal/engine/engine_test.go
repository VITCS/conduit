package engine

import (
	"context"
	"strings"
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
	// The approval node's strict predecessors must be done before it waits.
	// (m1 is a sibling branch and may still be running under parallel execution.)
	for _, id := range []string{"t1", "g1", "p1", "c1", "h1"} {
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

func TestRetriesBeforeFailing(t *testing.T) {
	st := store.NewMem()
	wf := model.Workflow{
		ID: "wf_retry", Name: "retry",
		Nodes: []model.Node{
			{ID: "t", Type: "github", Kind: model.KindTrigger, Title: "Trigger"},
			{ID: "h", Type: "http", Kind: model.KindStep, Title: "Flaky",
				Config: []model.Field{
					{Key: "url", Value: "http://svc/fail"},
					{Key: "retries", Value: "2"},
					{Key: "retry_backoff_ms", Value: "0"},
				}},
		},
		Edges: []model.Edge{{From: "t", To: "h"}},
	}
	if err := st.PutWorkflow(wf); err != nil {
		t.Fatal(err)
	}
	e := New(st)
	e.StepDelay, e.TriggerDelay = 0, 0

	// Count "retrying" log events emitted for the failing node.
	id, ch := st.Broker().Subscribe()
	defer st.Broker().Unsubscribe(id)
	retries := 0
	done := make(chan struct{})
	go func() {
		for ev := range ch {
			if ev.Type == "log" && ev.NodeID == "h" && strings.Contains(ev.Message, "retrying") {
				retries++
			}
			if ev.Type == "run.finished" {
				close(done)
				return
			}
		}
	}()

	run := e.Start(wf, "")
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not finish")
	}

	r := runStatus(t, st, run.ID)
	if r.Steps["h"].Status != model.StatusFailed {
		t.Errorf("h = %s, want failed", r.Steps["h"].Status)
	}
	if retries != 2 { // 2 retries after attempts 1 and 2; attempt 3 fails without retry
		t.Errorf("retry count = %d, want 2", retries)
	}
}

func TestCancelWaitingRun(t *testing.T) {
	e, st := newTestEngine(t)
	run := e.Start(seed.Workflow(), "")
	waitFor(t, 2*time.Second, func() bool {
		return runStatus(t, st, run.ID).Status == model.StatusWaiting
	})
	if !e.Cancel(run.ID) {
		t.Fatal("Cancel returned false for an active run")
	}
	waitFor(t, 2*time.Second, func() bool {
		return runStatus(t, st, run.ID).Status == model.StatusCanceled
	})
	r := runStatus(t, st, run.ID)
	if r.Steps["a1"].Status != model.StatusCanceled {
		t.Errorf("a1 = %s, want canceled", r.Steps["a1"].Status)
	}
	// Cancelling again is a no-op (run no longer active).
	if e.Cancel(run.ID) {
		t.Error("Cancel returned true for a finished run")
	}
}

func TestRetryConfig(t *testing.T) {
	n := model.Node{Config: []model.Field{{Key: "retries", Value: "3"}, {Key: "retry_backoff_ms", Value: "50"}}}
	attempts, backoff := retryConfig(n)
	if attempts != 4 || backoff != 50*time.Millisecond {
		t.Errorf("retryConfig = (%d, %v), want (4, 50ms)", attempts, backoff)
	}
	a2, b2 := retryConfig(model.Node{})
	if a2 != 1 || b2 != 200*time.Millisecond {
		t.Errorf("default retryConfig = (%d, %v), want (1, 200ms)", a2, b2)
	}
}

func TestWebhookInputReachesTrigger(t *testing.T) {
	e, st := newTestEngine(t)
	run := e.StartWithInput(seed.Workflow(), "", map[string]any{"pr": 154, "action": "opened"})
	waitFor(t, 2*time.Second, func() bool {
		s := runStatus(t, st, run.ID).Steps["t1"]
		return s != nil && s.Status == model.StatusTriggered
	})
	out := runStatus(t, st, run.ID).Steps["t1"].Output
	in, ok := out["input"].(map[string]any)
	if !ok {
		t.Fatalf("trigger output has no input map: %v", out)
	}
	if in["action"] != "opened" {
		t.Errorf("input.action = %v, want opened", in["action"])
	}
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestCancelQueuedRun verifies the worker-model cancel fix: a run canceled while
// still queued (before any worker claims it) is finalized as canceled and never
// executes, so the UI leaves the running state.
func TestCancelQueuedRun(t *testing.T) {
	e, st := newTestEngine(t)
	wf := seed.Workflow()

	run := e.Enqueue(wf, "", nil)
	if run.Status != model.StatusQueued {
		t.Fatalf("enqueued status = %s, want queued", run.Status)
	}
	if !e.Cancel(run.ID) {
		t.Fatal("Cancel(queued) returned false")
	}
	got := runStatus(t, st, run.ID)
	if got.Status != model.StatusCanceled {
		t.Fatalf("after cancel status = %s, want canceled", got.Status)
	}

	// A worker that nonetheless claims the (tombstoned) run must not execute it.
	e.ExecuteClaimed(context.Background(), run)
	got = runStatus(t, st, run.ID)
	if got.Status != model.StatusCanceled {
		t.Fatalf("after ExecuteClaimed status = %s, want still canceled", got.Status)
	}
	for id, s := range got.Steps {
		if s.Status == model.StatusCreated || s.Status == model.StatusSuccess {
			t.Fatalf("step %s = %s, want no execution after cancel", id, s.Status)
		}
	}
}

// TestCancelUnknownRun returns false for a run that does not exist.
func TestCancelUnknownRun(t *testing.T) {
	e, _ := newTestEngine(t)
	if e.Cancel("run_nope") {
		t.Fatal("Cancel(unknown) = true, want false")
	}
}

// Rerun re-executes from a failed node, keeping successful upstream work, and
// completes once the cause is fixed.
func TestRerunFromFailedNode(t *testing.T) {
	st := store.NewMem()
	wf := model.Workflow{
		ID: "wf_rr", Name: "rerun",
		Nodes: []model.Node{
			{ID: "t", Type: "github", Kind: model.KindTrigger, Title: "Trigger"},
			{ID: "a", Type: "http", Kind: model.KindStep, Title: "A", Config: []model.Field{{Key: "url", Value: "http://svc/ok"}}},
			{ID: "b", Type: "http", Kind: model.KindStep, Title: "B", Config: []model.Field{{Key: "url", Value: "http://svc/fail"}}},
			{ID: "c", Type: "http", Kind: model.KindStep, Title: "C", Config: []model.Field{{Key: "url", Value: "http://svc/ok"}}},
		},
		Edges: []model.Edge{
			{From: "t", To: "a"}, {From: "a", To: "b", Label: "success"}, {From: "b", To: "c", Label: "success"},
		},
	}
	if err := st.PutWorkflow(wf); err != nil {
		t.Fatal(err)
	}
	e := New(st)
	e.StepDelay, e.TriggerDelay = 0, 0

	run := e.Start(wf, "")
	waitFor(t, 2*time.Second, func() bool { return runStatus(t, st, run.ID).Status == model.StatusFailed })
	r := runStatus(t, st, run.ID)
	if r.Steps["a"].Status != model.StatusSuccess || r.Steps["b"].Status != model.StatusFailed {
		t.Fatalf("setup: a=%s b=%s", r.Steps["a"].Status, r.Steps["b"].Status)
	}
	aStarted := r.Steps["a"].StartedAt

	// Fix the cause (B's URL no longer names a failure), then re-run from B.
	wf.Nodes[2].Config[0].Value = "http://svc/ok"
	if err := st.PutWorkflow(wf); err != nil {
		t.Fatal(err)
	}
	snap, err := e.Rerun(run.ID, "b")
	if err != nil {
		t.Fatalf("rerun: %v", err)
	}
	if snap.Status != model.StatusQueued || len(snap.ResumeFrom) != 1 || snap.ResumeFrom[0] != "b" {
		t.Fatalf("rerun state: status=%s resume=%v", snap.Status, snap.ResumeFrom)
	}
	if snap.Steps["b"].Status != model.StatusPending || snap.Steps["c"].Status != model.StatusPending {
		t.Fatalf("b/c not reset: b=%s c=%s", snap.Steps["b"].Status, snap.Steps["c"].Status)
	}
	if snap.Steps["a"].Status != model.StatusSuccess {
		t.Fatalf("a should be preserved, got %s", snap.Steps["a"].Status)
	}

	// Drive a worker claim (production path) and let it finish.
	claimed, ok, _ := st.ClaimRun("w1", time.Second)
	if !ok || claimed.ID != run.ID {
		t.Fatalf("claim: ok=%v id=%s", ok, claimed.ID)
	}
	e.ExecuteClaimed(context.Background(), claimed)

	waitFor(t, 2*time.Second, func() bool { return runStatus(t, st, run.ID).Status == model.StatusSuccess })
	r = runStatus(t, st, run.ID)
	if r.Steps["b"].Status != model.StatusSuccess || r.Steps["c"].Status != model.StatusSuccess {
		t.Errorf("after rerun: b=%s c=%s, want success", r.Steps["b"].Status, r.Steps["c"].Status)
	}
	// A was not re-executed: its start time is unchanged from the first run.
	if a := r.Steps["a"].StartedAt; (a == nil) != (aStarted == nil) || (a != nil && !a.Equal(*aStarted)) {
		t.Errorf("A was re-run: start changed %v -> %v", aStarted, a)
	}
	if len(r.ResumeFrom) != 0 {
		t.Errorf("ResumeFrom should be consumed, got %v", r.ResumeFrom)
	}
}

// Rerun is refused while a run is still active.
func TestRerunRejectsActiveRun(t *testing.T) {
	e, st := newTestEngine(t)
	run := e.Start(seed.Workflow(), "")
	waitFor(t, 2*time.Second, func() bool { return runStatus(t, st, run.ID).Status == model.StatusWaiting })
	if _, err := e.Rerun(run.ID, "a1"); err == nil {
		t.Error("expected rerun to be refused for an active (waiting) run")
	}
}

// A disabled step is bypassed: it is not executed, is marked skipped, and the run
// continues down its success edge.
func TestDisabledNodeIsBypassed(t *testing.T) {
	st := store.NewMem()
	wf := model.Workflow{
		ID: "wf_bypass", Name: "bypass",
		Nodes: []model.Node{
			{ID: "t", Type: "github", Kind: model.KindTrigger, Title: "Trigger"},
			{ID: "a", Type: "http", Kind: model.KindStep, Title: "A", Config: []model.Field{{Key: "url", Value: "http://svc/ok"}}},
			// This node would FAIL if executed (url names a failure), but it is disabled.
			{ID: "chk", Type: "http", Kind: model.KindStep, Title: "Verify", Disabled: true,
				Config: []model.Field{{Key: "url", Value: "http://svc/fail"}}},
			{ID: "pr", Type: "status", Kind: model.KindStatus, Title: "PR"},
		},
		Edges: []model.Edge{
			{From: "t", To: "a"}, {From: "a", To: "chk", Label: "success"}, {From: "chk", To: "pr", Label: "success"},
		},
	}
	if err := st.PutWorkflow(wf); err != nil {
		t.Fatal(err)
	}
	e := New(st)
	e.StepDelay, e.TriggerDelay = 0, 0
	run := e.Start(wf, "")

	waitFor(t, 2*time.Second, func() bool {
		s := runStatus(t, st, run.ID).Status
		return s == model.StatusSuccess || s == model.StatusFailed
	})
	r := runStatus(t, st, run.ID)
	if r.Status != model.StatusSuccess {
		t.Fatalf("run = %s, want success (disabled check must not fail the run)", r.Status)
	}
	if r.Steps["chk"].Status != model.StatusSkipped {
		t.Errorf("chk = %s, want skipped (bypassed)", r.Steps["chk"].Status)
	}
	if r.Steps["chk"].Output["bypassed"] != true {
		t.Errorf("chk output should mark bypassed, got %v", r.Steps["chk"].Output)
	}
	if r.Steps["pr"].Status == model.StatusSkipped {
		t.Errorf("pr should have run after the bypassed step, got %s", r.Steps["pr"].Status)
	}
}

// Canceling during a bounded recovery loop (CI fails → retry Apply) must stop the
// loop and finalize the run as canceled, not keep looping back.
func TestCancelStopsRecoveryLoop(t *testing.T) {
	st := store.NewMem()
	wf := model.Workflow{
		ID: "wf_cancel_loop", Name: "cancel loop",
		Nodes: []model.Node{
			{ID: "t", Type: "github", Kind: model.KindTrigger, Title: "Trigger"},
			{ID: "apply", Type: "http", Kind: model.KindStep, Title: "Apply", Config: []model.Field{{Key: "url", Value: "http://svc/ok"}}},
			{ID: "ci", Type: "http", Kind: model.KindStep, Title: "CI", Config: []model.Field{{Key: "url", Value: "http://svc/fail"}}},
		},
		Edges: []model.Edge{
			{From: "t", To: "apply"},
			{From: "apply", To: "ci", Label: "success"},
			{From: "ci", To: "apply", Label: "failed", MaxAttempts: 100}, // recovery back-edge
		},
	}
	if err := st.PutWorkflow(wf); err != nil {
		t.Fatal(err)
	}
	e := New(st)
	e.StepDelay, e.TriggerDelay = 15*time.Millisecond, 0
	run := e.Start(wf, "")

	// Let the loop iterate a few times, then cancel.
	time.Sleep(90 * time.Millisecond)
	e.Cancel(run.ID)

	waitFor(t, 3*time.Second, func() bool {
		s := runStatus(t, st, run.ID).Status
		return s == model.StatusCanceled || s == model.StatusFailed || s == model.StatusSuccess
	})
	if got := runStatus(t, st, run.ID).Status; got != model.StatusCanceled {
		t.Fatalf("run = %s, want canceled (cancel must break the retry loop)", got)
	}
}

// The engine must broadcast a node's running transition as a step.updated event
// (status=running) so the UI shows "In progress" live, not only after the node
// finishes or on a page refresh.
func TestRunningTransitionIsBroadcast(t *testing.T) {
	st := store.NewMem()
	wf := model.Workflow{
		ID: "wf_live", Name: "live",
		Nodes: []model.Node{
			{ID: "t", Type: "github", Kind: model.KindTrigger, Title: "Trigger"},
			{ID: "h", Type: "http", Kind: model.KindStep, Title: "Step", Config: []model.Field{{Key: "url", Value: "http://svc/ok"}}},
		},
		Edges: []model.Edge{{From: "t", To: "h"}},
	}
	if err := st.PutWorkflow(wf); err != nil {
		t.Fatal(err)
	}
	e := New(st)
	e.StepDelay, e.TriggerDelay = 0, 0

	id, ch := st.Broker().Subscribe()
	defer st.Broker().Unsubscribe(id)
	sawRunning := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		for ev := range ch {
			if ev.Type == "step.updated" && ev.NodeID == "h" && ev.Status == model.StatusRunning {
				select {
				case sawRunning <- struct{}{}:
				default:
				}
			}
			if ev.Type == "run.finished" {
				close(done)
				return
			}
		}
	}()

	e.Start(wf, "")
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not finish")
	}
	select {
	case <-sawRunning:
	default:
		t.Error("no step.updated(status=running) was broadcast for node h")
	}
}
