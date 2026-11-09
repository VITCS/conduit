package engine

import (
	"testing"
	"time"

	"conduit/internal/components"
	"conduit/internal/model"
	"conduit/internal/store"
)

// flaky fails until memory["flaky_n"] reaches the node's fail_times, incrementing
// it on each failure. It lets a test drive a bounded recovery loop deterministically.
func registerFlaky() {
	components.Register("flaky", func(n model.Node, run *model.Run) components.Result {
		target := 0
		for _, f := range n.Config {
			if f.Key == "fail_times" {
				for _, c := range f.Value {
					target = target*10 + int(c-'0')
				}
			}
		}
		cur := 0
		switch v := run.Memory["flaky_n"].(type) {
		case int:
			cur = v
		case float64:
			cur = int(v)
		}
		if cur < target {
			return components.Result{Outcome: model.StatusFailed,
				MemoryWrites: map[string]any{"flaky_n": cur + 1},
				Output:       map[string]any{"attempt": cur + 1}}
		}
		return components.Result{Outcome: model.StatusSuccess, Output: map[string]any{"attempt": cur}}
	})
}

// loopWF: trigger → impl(noop) → vrf(flaky) → ok, with a bounded retry back-edge
// vrf→impl and a terminal vrf→bad for when the budget is spent.
func loopWF(failTimes, maxAttempts int) model.Workflow {
	ft := string(rune('0' + failTimes))
	return model.Workflow{
		ID: "wf_loop", ProjectID: "default", Name: "loop",
		Nodes: []model.Node{
			{ID: "t1", Type: "webhook", Kind: model.KindTrigger, Title: "T"},
			{ID: "impl", Type: "noop", Kind: model.KindStep, Title: "Implement"},
			{ID: "vrf", Type: "flaky", Kind: model.KindStep, Title: "Verify",
				Config: []model.Field{{Key: "fail_times", Value: ft}}},
			{ID: "ok", Type: "status", Kind: model.KindStatus, Title: "Done"},
			{ID: "bad", Type: "status", Kind: model.KindStatus, Title: "Needs attention"},
		},
		Edges: []model.Edge{
			{From: "t1", To: "impl"},
			{From: "impl", To: "vrf", Label: "success"},
			{From: "vrf", To: "ok", Label: "success"},
			{From: "vrf", To: "impl", Label: "failed", MaxAttempts: maxAttempts},
			{From: "vrf", To: "bad", Label: "failed"},
		},
	}
}

func newLoopEngine(t *testing.T) (*Engine, store.Store) {
	t.Helper()
	registerFlaky()
	st := store.NewMem()
	e := New(st)
	e.StepDelay = 0
	e.TriggerDelay = 0
	return e, st
}

func TestRecoveryLoopRecovers(t *testing.T) {
	e, st := newLoopEngine(t)
	run := e.Start(loopWF(2, 3), "") // fails twice, then succeeds; budget 3

	waitFor(t, 3*time.Second, func() bool {
		return isTerminal(runStatus(t, st, run.ID).Status)
	})
	r := runStatus(t, st, run.ID)
	if r.Status != model.StatusSuccess {
		t.Fatalf("want success after recovery, got %s", r.Status)
	}
	if got := r.Loops["vrf\x00impl"]; got != 2 {
		t.Errorf("want 2 retries, got %d", got)
	}
	if st := r.Steps["ok"].Status; st == model.StatusPending || st == model.StatusSkipped {
		t.Errorf("Done step not reached: %s", st)
	}
	if r.Steps["vrf"].Status != model.StatusSuccess {
		t.Errorf("Verify step = %s, want success", r.Steps["vrf"].Status)
	}
}

func TestRecoveryLoopBudgetExhausted(t *testing.T) {
	e, st := newLoopEngine(t)
	run := e.Start(loopWF(9, 2), "") // always fails; budget 2 → then terminal

	waitFor(t, 3*time.Second, func() bool {
		return isTerminal(runStatus(t, st, run.ID).Status)
	})
	r := runStatus(t, st, run.ID)
	if r.Status != model.StatusFailed {
		t.Fatalf("want failed after budget exhausted, got %s", r.Status)
	}
	if got := r.Loops["vrf\x00impl"]; got != 2 {
		t.Errorf("want exactly 2 retries (the budget), got %d", got)
	}
	if r.Steps["bad"].Status == model.StatusPending || r.Steps["bad"].Status == model.StatusSkipped {
		t.Errorf("terminal 'Needs attention' not reached: %s", r.Steps["bad"].Status)
	}
}

func isTerminal(s model.Status) bool {
	return s == model.StatusSuccess || s == model.StatusFailed || s == model.StatusCanceled
}
