package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"conduit/internal/components"
	"conduit/internal/model"
)

// A step that hangs is nudged once, then flagged stuck; progress clears the mark;
// finishing the step removes it.
func TestWatchdogNudgesThenFlagsAStuckStep(t *testing.T) {
	release := make(chan struct{})
	components.Register("test_hang", func(n model.Node, run *model.Run) components.Result {
		<-release
		return components.Result{Outcome: model.StatusSuccess, Output: map[string]any{"done": true}}
	})
	e, st := newTestEngine(t)
	e.StallNudge, e.StallStuck, e.WatchInterval = 60*time.Millisecond, 160*time.Millisecond, 10*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go e.Watch(ctx)

	var stalls atomic.Int32
	subID, sub := st.Broker().Subscribe()
	defer st.Broker().Unsubscribe(subID)
	go func() {
		for ev := range sub {
			if ev.Type == "step.stalled" {
				stalls.Add(1)
			}
		}
	}()

	wf := model.Workflow{ID: "wf_hang", ProjectID: model.DefaultProjectID, Name: "hang",
		Nodes: []model.Node{
			{ID: "t1", Type: "start", Kind: model.KindTrigger, Title: "Start"},
			{ID: "ag", Type: "test_hang", Kind: model.KindStep, Title: "Apply changes"},
		},
		Edges: []model.Edge{{From: "t1", To: "ag"}}}
	if err := st.PutWorkflow(wf); err != nil {
		t.Fatal(err)
	}
	run := e.Start(wf, "")
	stall := func() any {
		r := runStatus(t, st, run.ID)
		if s := r.Steps["ag"]; s != nil {
			return s.Output["stall"]
		}
		return nil
	}

	waitFor(t, 2*time.Second, func() bool { return stall() == StallSlow })
	waitFor(t, 2*time.Second, func() bool { return stall() == StallStuck })
	if r := runStatus(t, st, run.ID); r.Steps["ag"].Output["stall_since"] == nil || r.Status != model.StatusRunning {
		t.Fatalf("stuck step: %+v status %s", r.Steps["ag"], r.Status)
	}
	time.Sleep(50 * time.Millisecond)
	if n := stalls.Load(); n != 2 {
		t.Errorf("want one nudge and one stuck event, got %d", n)
	}

	// Progress clears the mark.
	e.touch(run.ID, "ag")
	if s := stall(); s != nil {
		t.Fatalf("progress should clear the stall mark, got %v", s)
	}

	close(release)
	waitFor(t, 2*time.Second, func() bool { return runStatus(t, st, run.ID).Status == model.StatusSuccess })
	if s := stall(); s != nil {
		t.Errorf("a finished step has no stall mark, got %v", s)
	}
	e.mu.Lock()
	n := len(e.active)
	e.mu.Unlock()
	if n != 0 {
		t.Errorf("finished steps must not be watched, %d still tracked", n)
	}
}

// Steps waiting on a person (a question) are not watched.
func TestWatchdogIgnoresStepsWaitingOnAPerson(t *testing.T) {
	e, get := startQuestion(t)
	e.StallNudge, e.StallStuck = time.Nanosecond, 2*time.Nanosecond
	e.checkStalls(time.Now().Add(time.Hour))
	if s := get().Steps["ag"].Output["stall"]; s != nil {
		t.Fatalf("a step waiting for an answer is not stuck, got %v", s)
	}
}

// Canceling a run stops a step blocked in a model call right away (it does not
// wait for the HTTP timeout).
func TestCancelStopsAHangingModelCall(t *testing.T) {
	stop := make(chan struct{})
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-stop }))
	defer hang.Close()
	defer close(stop)
	old := components.AllowPrivateEgress
	components.AllowPrivateEgress = true
	defer func() { components.AllowPrivateEgress = old }()

	e, st := newTestEngine(t)
	wf := model.Workflow{ID: "wf_llm", ProjectID: model.DefaultProjectID, Name: "llm",
		Nodes: []model.Node{
			{ID: "t1", Type: "start", Kind: model.KindTrigger, Title: "Start"},
			{ID: "ag", Type: "agent", Kind: model.KindStep, Title: "Plan", Config: []model.Field{
				{Key: "live", Value: "true"}, {Key: "provider", Value: "custom"}, {Key: "base_url", Value: hang.URL},
				{Key: "ask", Value: "false"}, {Key: "prompt", Value: "x"}}},
		},
		Edges: []model.Edge{{From: "t1", To: "ag"}}}
	_ = st.PutWorkflow(wf)
	run := e.Start(wf, "")
	waitFor(t, 2*time.Second, func() bool {
		s := runStatus(t, st, run.ID).Steps["ag"]
		return s != nil && s.Status == model.StatusRunning
	})
	time.Sleep(50 * time.Millisecond)
	start := time.Now()
	e.Cancel(run.ID)
	waitFor(t, 3*time.Second, func() bool { return runStatus(t, st, run.ID).Status == model.StatusCanceled })
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("cancel took %s", d)
	}
}
