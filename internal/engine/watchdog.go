package engine

// Stuck-run detection. While a step runs, the engine records its last sign of
// progress: the step starting, and every progress report (a Claude Code tool use,
// a file being written). A watchdog checks once a minute:
//
//   - no progress for StallNudge (default 5 min): the step is nudged once — a
//     warning in the run log, a "slow" mark on the step and a notification;
//   - no progress for StallStuck (default 10 min): the step is marked stuck and
//     the run shows in "Needs you" with a stuck label, so a person can look at it
//     or cancel it.
//
// Progress clears both marks. Steps waiting on a person (approval gates and
// questions) are not watched: they are already in "Needs you".

import (
	"context"
	"fmt"
	"time"

	"conduit/internal/model"
)

// Stall marks, stored on a running step's output.
const (
	StallSlow  = "slow"
	StallStuck = "stuck"
)

type activity struct {
	runID, nodeID, title string
	last                 time.Time
	nudged, stuck        bool
}

func actKey(runID, nodeID string) string { return runID + "|" + nodeID }

// track starts watching a running step.
func (e *Engine) track(runID string, node model.Node) {
	e.mu.Lock()
	e.active[actKey(runID, node.ID)] = &activity{runID: runID, nodeID: node.ID, title: node.Title, last: time.Now()}
	e.mu.Unlock()
}

// untrack stops watching a step (it finished, failed, or now waits on a person).
func (e *Engine) untrack(runID, nodeID string) {
	e.mu.Lock()
	delete(e.active, actKey(runID, nodeID))
	e.mu.Unlock()
}

// touch records progress on a running step, and clears a slow/stuck mark.
func (e *Engine) touch(runID, nodeID string) {
	e.mu.Lock()
	a := e.active[actKey(runID, nodeID)]
	flagged := false
	if a != nil {
		a.last = time.Now()
		flagged = a.nudged || a.stuck
		a.nudged, a.stuck = false, false
	}
	e.mu.Unlock()
	if flagged {
		e.setStall(runID, nodeID, "", time.Time{})
		e.publish(model.Event{Type: "log", RunID: runID, NodeID: nodeID, Level: "run",
			Message: fmt.Sprintf("%s · progress again", a.title)})
	}
}

// Watch runs the stuck-step watchdog until ctx ends.
func (e *Engine) Watch(ctx context.Context) {
	t := time.NewTicker(e.WatchInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.checkStalls(time.Now())
		}
	}
}

// checkStalls nudges or flags steps that have made no progress for too long.
func (e *Engine) checkStalls(now time.Time) {
	type flag struct {
		a     activity
		level string
	}
	var flags []flag
	e.mu.Lock()
	for _, a := range e.active {
		idle := now.Sub(a.last)
		switch {
		case idle >= e.StallStuck && !a.stuck:
			a.stuck, a.nudged = true, true
			flags = append(flags, flag{*a, StallStuck})
		case idle >= e.StallNudge && !a.nudged:
			a.nudged = true
			flags = append(flags, flag{*a, StallSlow})
		}
	}
	e.mu.Unlock()
	for _, f := range flags {
		mins := int(now.Sub(f.a.last).Minutes())
		msg := fmt.Sprintf("%s · no progress for %d min — still running; it will be flagged as stuck after %d min",
			f.a.title, mins, int(e.StallStuck.Minutes()))
		if f.level == StallStuck {
			msg = fmt.Sprintf("%s · no progress for %d min — marked stuck; it is in Needs you", f.a.title, mins)
		}
		e.setStall(f.a.runID, f.a.nodeID, f.level, f.a.last)
		e.publish(model.Event{Type: "step.stalled", RunID: f.a.runID, NodeID: f.a.nodeID, Level: "warn",
			Status: model.StatusRunning, Message: msg, Detail: f.level, Step: e.stepSnap(f.a.runID, f.a.nodeID)})
		e.publish(model.Event{Type: "log", RunID: f.a.runID, NodeID: f.a.nodeID, Level: "warn", Message: msg})
	}
}

// setStall writes (or, with level "", clears) the stall mark on a running step.
func (e *Engine) setStall(runID, nodeID, level string, since time.Time) {
	e.store.MutateRun(runID, func(r *model.Run) { //nolint:errcheck // best-effort
		s := r.Steps[nodeID]
		if s == nil || s.Status != model.StatusRunning {
			return
		}
		if level == "" {
			delete(s.Output, "stall")
			delete(s.Output, "stall_since")
			return
		}
		if s.Output == nil {
			s.Output = map[string]any{}
		}
		s.Output["stall"] = level
		s.Output["stall_since"] = since.UTC().Format(time.RFC3339)
	})
}
