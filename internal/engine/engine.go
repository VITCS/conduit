// Package engine executes a workflow as a run. It walks the DAG from the trigger
// nodes, runs each component, and follows outgoing edges whose label matches the
// node's outcome (success / failed / approved). Approval nodes pause the run
// until a human decision arrives via Resolve.
//
// Each node emits live events (running -> logs -> final) through the store's
// broker so the UI reflects progress as it happens.
package engine

import (
	"fmt"
	"sync"
	"time"

	"conduit/internal/components"
	"conduit/internal/model"
	"conduit/internal/store"
)

// Engine runs workflows against a store.
type Engine struct {
	store store.Store

	mu        sync.Mutex
	approvals map[string]chan string // runID|nodeID -> decision channel

	// pacing between steps; exported-ish knobs kept small for a responsive demo.
	StepDelay    time.Duration
	TriggerDelay time.Duration
}

// New builds an engine bound to a store.
func New(s store.Store) *Engine {
	return &Engine{
		store:        s,
		approvals:    map[string]chan string{},
		StepDelay:    650 * time.Millisecond,
		TriggerDelay: 300 * time.Millisecond,
	}
}

var runSeq struct {
	sync.Mutex
	n int
}

func nextRunID() string {
	runSeq.Lock()
	defer runSeq.Unlock()
	runSeq.n++
	return fmt.Sprintf("run_%d", int(time.Now().Unix()%100000)*100+runSeq.n)
}

// Start creates a run for a workflow and executes it in the background.
func (e *Engine) Start(wf model.Workflow, title string) model.Run {
	if title == "" {
		title = wf.Name
	}
	r := &model.Run{
		ID:         nextRunID(),
		WorkflowID: wf.ID,
		Title:      title,
		Status:     model.StatusRunning,
		CreatedAt:  time.Now(),
		Steps:      map[string]*model.StepRun{},
		Memory:     map[string]any{},
	}
	for _, n := range wf.Nodes {
		r.Steps[n.ID] = &model.StepRun{NodeID: n.ID, Status: model.StatusPending}
	}
	snap, _ := e.store.CreateRun(r)
	go e.execute(r.ID, wf)
	return snap
}

// Resolve delivers a human decision ("approved"/"rejected") to a waiting
// approval node. Returns false if nothing is waiting on that node.
func (e *Engine) Resolve(runID, nodeID, decision string) bool {
	e.mu.Lock()
	ch, ok := e.approvals[key(runID, nodeID)]
	e.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- decision:
		return true
	default:
		return false
	}
}

func key(runID, nodeID string) string { return runID + "|" + nodeID }

func (e *Engine) execute(runID string, wf model.Workflow) {
	nodes := map[string]model.Node{}
	for _, n := range wf.Nodes {
		nodes[n.ID] = n
	}
	out := map[string][]model.Edge{}
	for _, ed := range wf.Edges {
		out[ed.From] = append(out[ed.From], ed)
	}

	e.publish(model.Event{Type: "run.started", RunID: runID, Level: "info",
		Message: fmt.Sprintf("Run %s started", runID)})

	// Breadth-first walk from trigger nodes. Sequential execution keeps step
	// ordering legible; a parallel scheduler is a natural next step.
	queue := []string{}
	for _, n := range wf.Nodes {
		if n.Kind == model.KindTrigger {
			queue = append(queue, n.ID)
		}
	}
	visited := map[string]bool{}
	failed := false

	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if visited[id] {
			continue
		}
		visited[id] = true

		node, ok := nodes[id]
		if !ok {
			continue
		}

		outcome := e.runNode(runID, node)
		if outcome == model.StatusFailed {
			failed = true
		}
		for _, ed := range out[id] {
			if edgeMatches(ed.Label, outcome) {
				queue = append(queue, ed.To)
			}
		}
	}

	// Anything never reached on a not-taken branch is marked skipped.
	e.store.MutateRun(runID, func(r *model.Run) {
		for nid, st := range r.Steps {
			if !visited[nid] && st.Status == model.StatusPending {
				st.Status = model.StatusSkipped
			}
		}
	})

	final := model.StatusSuccess
	if failed {
		final = model.StatusFailed
	}
	snap, _, _ := e.store.MutateRun(runID, func(r *model.Run) {
		r.Status = final
		now := time.Now()
		r.FinishedAt = &now
	})
	e.publish(model.Event{Type: "run.finished", RunID: runID, Status: final,
		Level: levelFor(final), Message: fmt.Sprintf("Run %s %s", runID, final),
		Run: &snap})
}

// runNode executes one node and returns its outcome. Approval nodes block here
// until Resolve is called.
func (e *Engine) runNode(runID string, node model.Node) model.Status {
	// mark running
	start := time.Now()
	e.setStep(runID, node.ID, model.StatusRunning, func(s *model.StepRun) {
		s.StartedAt = &start
	})
	e.publish(model.Event{Type: "log", RunID: runID, NodeID: node.ID, Level: "run",
		Message: fmt.Sprintf("%s · running", node.Title)})

	if node.Type == "approval" {
		return e.runApproval(runID, node)
	}

	time.Sleep(e.stepPacing(node))

	// execute against the live run (executors may touch run.Memory)
	var res components.Result
	e.store.MutateRun(runID, func(r *model.Run) {
		res = components.For(node.Type)(node, r)
	}) //nolint:errcheck // best-effort during execution

	status := statusFor(res.Outcome)
	end := time.Now()
	e.setStep(runID, node.ID, status, func(s *model.StepRun) {
		s.EndedAt = &end
		s.Output = res.Output
		s.Logs = res.Logs
	})
	for _, l := range res.Logs {
		e.publish(model.Event{Type: "log", RunID: runID, NodeID: node.ID,
			Level: levelFor(status), Message: fmt.Sprintf("%s · %s", node.Title, l)})
	}
	snap := e.stepSnap(runID, node.ID)
	e.publish(model.Event{Type: "step.updated", RunID: runID, NodeID: node.ID,
		Status: status, Level: levelFor(status), Step: snap,
		Message: fmt.Sprintf("%s · %s", node.Title, status)})
	return res.Outcome
}

func (e *Engine) runApproval(runID string, node model.Node) model.Status {
	ch := make(chan string, 1)
	e.mu.Lock()
	e.approvals[key(runID, node.ID)] = ch
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.approvals, key(runID, node.ID))
		e.mu.Unlock()
	}()

	e.setStep(runID, node.ID, model.StatusWaiting, nil)
	snap, _, _ := e.store.MutateRun(runID, func(r *model.Run) { r.Status = model.StatusWaiting })
	e.publish(model.Event{Type: "run.waiting", RunID: runID, NodeID: node.ID,
		Status: model.StatusWaiting, Level: "info",
		Message: fmt.Sprintf("%s · awaiting approval from %s", node.Title, node.Field("approvers")),
		Run:     &snap})

	decision := <-ch

	e.store.MutateRun(runID, func(r *model.Run) { r.Status = model.StatusRunning }) //nolint:errcheck

	end := time.Now()
	if decision == "approved" {
		e.setStep(runID, node.ID, model.StatusSuccess, func(s *model.StepRun) {
			s.EndedAt = &end
			s.Output = map[string]any{"decision": "approved", "approvers": node.Field("approvers")}
			s.Logs = []string{"approved"}
		})
		e.publishStep(runID, node.ID, model.StatusSuccess, node.Title+" · approved")
		return model.StatusSuccess
	}
	e.setStep(runID, node.ID, model.StatusFailed, func(s *model.StepRun) {
		s.EndedAt = &end
		s.Output = map[string]any{"decision": "rejected"}
		s.Logs = []string{"rejected"}
	})
	e.publishStep(runID, node.ID, model.StatusFailed, node.Title+" · rejected")
	return model.StatusFailed
}

// --- helpers ---

func (e *Engine) stepPacing(node model.Node) time.Duration {
	if node.Kind == model.KindTrigger {
		return e.TriggerDelay
	}
	if node.Type == "memory" || node.Type == "status" {
		return e.StepDelay / 2
	}
	return e.StepDelay
}

func (e *Engine) setStep(runID, nodeID string, status model.Status, extra func(*model.StepRun)) {
	e.store.MutateRun(runID, func(r *model.Run) {
		s := r.Steps[nodeID]
		if s == nil {
			s = &model.StepRun{NodeID: nodeID}
			r.Steps[nodeID] = s
		}
		s.Status = status
		if extra != nil {
			extra(s)
		}
	}) //nolint:errcheck // best-effort during execution
}

func (e *Engine) stepSnap(runID, nodeID string) *model.StepRun {
	snap, ok, _ := e.store.GetRun(runID)
	if !ok {
		return nil
	}
	return snap.Steps[nodeID]
}

func (e *Engine) publishStep(runID, nodeID string, status model.Status, msg string) {
	e.publish(model.Event{Type: "step.updated", RunID: runID, NodeID: nodeID,
		Status: status, Level: levelFor(status), Step: e.stepSnap(runID, nodeID), Message: msg})
}

func (e *Engine) publish(ev model.Event) {
	ev.Time = time.Now()
	e.store.Broker().Publish(ev)
}

// edgeMatches decides whether an outgoing edge is taken for a given outcome.
// A successful outcome (including an approved gate, which resolves to success)
// follows unlabeled, "success", and "approved" edges; a failure follows
// "failed" edges only.
func edgeMatches(label string, outcome model.Status) bool {
	if outcome == model.StatusFailed {
		return label == "failed"
	}
	return label == "" || label == "success" || label == "approved"
}

// statusFor maps a component outcome to the step status recorded for it.
func statusFor(outcome model.Status) model.Status {
	switch outcome {
	case model.StatusFailed:
		return model.StatusFailed
	case model.StatusCreated:
		return model.StatusCreated
	case model.StatusTriggered:
		return model.StatusTriggered
	default:
		return model.StatusSuccess
	}
}

func levelFor(s model.Status) string {
	switch s {
	case model.StatusFailed:
		return "fail"
	case model.StatusSuccess, model.StatusCreated:
		return "ok"
	case model.StatusRunning:
		return "run"
	default:
		return "info"
	}
}
