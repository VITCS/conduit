// Package engine executes a workflow as a run. It walks the DAG from the trigger
// nodes, runs each component, and follows outgoing edges whose label matches the
// node's outcome (success / failed / approved). Approval nodes pause the run
// until a human decision arrives via Resolve.
//
// Each node emits live events (running -> logs -> final) through the store's
// broker so the UI reflects progress as it happens.
package engine

import (
	"context"
	"fmt"
	"strconv"
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
	approvals map[string]chan string        // runID|nodeID -> decision channel
	cancels   map[string]context.CancelFunc // runID -> cancel

	// pacing between steps; exported-ish knobs kept small for a responsive demo.
	StepDelay    time.Duration
	TriggerDelay time.Duration
}

// New builds an engine bound to a store.
func New(s store.Store) *Engine {
	return &Engine{
		store:        s,
		approvals:    map[string]chan string{},
		cancels:      map[string]context.CancelFunc{},
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
	return e.StartWithInput(wf, title, nil)
}

// StartWithInput is Start with a payload (e.g. a webhook body) bound to the run;
// the trigger node exposes it to downstream steps.
func (e *Engine) StartWithInput(wf model.Workflow, title string, input map[string]any) model.Run {
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
		Input:      input,
	}
	for _, n := range wf.Nodes {
		r.Steps[n.ID] = &model.StepRun{NodeID: n.ID, Status: model.StatusPending}
	}
	snap, _ := e.store.CreateRun(r)

	ctx, cancel := context.WithCancel(context.Background())
	e.mu.Lock()
	e.cancels[r.ID] = cancel
	e.mu.Unlock()

	go e.execute(ctx, r.ID, wf)
	return snap
}

// Cancel stops a running or waiting run. Returns false if no such run is active.
func (e *Engine) Cancel(runID string) bool {
	e.mu.Lock()
	cancel, ok := e.cancels[runID]
	e.mu.Unlock()
	if !ok {
		return false
	}
	cancel()
	return true
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

func (e *Engine) execute(ctx context.Context, runID string, wf model.Workflow) {
	// Release the cancel registration when the run ends.
	defer func() {
		e.mu.Lock()
		delete(e.cancels, runID)
		e.mu.Unlock()
	}()

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

	// Parallel scheduler: a node is activated by the first incoming edge whose
	// label matches its predecessor's outcome, and runs exactly once. Independent
	// branches run concurrently in their own goroutines. (A true join — waiting
	// for all predecessors — is a later addition; "merge" is a pass-through today.)
	var (
		mu       sync.Mutex
		started  = map[string]bool{}
		wg       sync.WaitGroup
		failed   bool
		canceled bool
	)

	var activate func(id string)
	activate = func(id string) {
		mu.Lock()
		if started[id] {
			mu.Unlock()
			return
		}
		node, ok := nodes[id]
		if !ok {
			mu.Unlock()
			return
		}
		started[id] = true
		mu.Unlock()

		wg.Add(1)
		go func() {
			defer wg.Done()
			outcome := e.runNode(ctx, runID, node)
			mu.Lock()
			switch outcome {
			case model.StatusCanceled:
				canceled = true
			case model.StatusFailed:
				failed = true
			}
			mu.Unlock()
			if outcome == model.StatusCanceled {
				return // stop propagating this branch
			}
			for _, ed := range out[id] {
				if edgeMatches(ed.Label, outcome) {
					activate(ed.To)
				}
			}
		}()
	}

	for _, n := range wf.Nodes {
		if n.Kind == model.KindTrigger {
			activate(n.ID)
		}
	}
	wg.Wait()

	// Unreached nodes: skipped normally, or canceled if the run was stopped.
	leftover := model.StatusSkipped
	if canceled {
		leftover = model.StatusCanceled
	}
	e.store.MutateRun(runID, func(r *model.Run) {
		for nid, st := range r.Steps {
			if !started[nid] && (st.Status == model.StatusPending || st.Status == model.StatusWaiting) {
				st.Status = leftover
			}
		}
	})

	final := model.StatusSuccess
	switch {
	case canceled:
		final = model.StatusCanceled
	case failed:
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

// runNode executes one node and returns its outcome. It retries a failed
// executor up to the node's configured attempts, and returns StatusCanceled if
// the run's context is canceled. Approval nodes block until Resolve or cancel.
func (e *Engine) runNode(ctx context.Context, runID string, node model.Node) model.Status {
	start := time.Now()
	e.setStep(runID, node.ID, model.StatusRunning, func(s *model.StepRun) {
		s.StartedAt = &start
	})
	e.publish(model.Event{Type: "log", RunID: runID, NodeID: node.ID, Level: "run",
		Message: fmt.Sprintf("%s · running", node.Title)})

	if node.Type == "approval" {
		return e.runApproval(ctx, runID, node)
	}

	attempts, backoff := retryConfig(node)
	var res components.Result
	var logs []string
	for attempt := 1; attempt <= attempts; attempt++ {
		if !sleepCtx(ctx, e.stepPacing(node)) {
			return e.finishCanceled(runID, node)
		}
		e.store.MutateRun(runID, func(r *model.Run) {
			res = components.For(node.Type)(node, r)
		}) //nolint:errcheck // best-effort during execution
		logs = res.Logs
		if res.Outcome != model.StatusFailed || attempt == attempts {
			break
		}
		e.publish(model.Event{Type: "log", RunID: runID, NodeID: node.ID, Level: "run",
			Message: fmt.Sprintf("%s · attempt %d failed, retrying", node.Title, attempt)})
		if !sleepCtx(ctx, backoff*time.Duration(attempt)) {
			return e.finishCanceled(runID, node)
		}
	}

	status := statusFor(res.Outcome)
	end := time.Now()
	e.setStep(runID, node.ID, status, func(s *model.StepRun) {
		s.EndedAt = &end
		s.Output = res.Output
		s.Logs = logs
	})
	for _, l := range logs {
		e.publish(model.Event{Type: "log", RunID: runID, NodeID: node.ID,
			Level: levelFor(status), Message: fmt.Sprintf("%s · %s", node.Title, l)})
	}
	snap := e.stepSnap(runID, node.ID)
	e.publish(model.Event{Type: "step.updated", RunID: runID, NodeID: node.ID,
		Status: status, Level: levelFor(status), Step: snap,
		Message: fmt.Sprintf("%s · %s", node.Title, status)})
	return res.Outcome
}

// finishCanceled marks a node canceled and returns the canceled outcome.
func (e *Engine) finishCanceled(runID string, node model.Node) model.Status {
	e.setStep(runID, node.ID, model.StatusCanceled, nil)
	e.publishStep(runID, node.ID, model.StatusCanceled, node.Title+" · canceled")
	return model.StatusCanceled
}

func (e *Engine) runApproval(ctx context.Context, runID string, node model.Node) model.Status {
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

	var decision string
	select {
	case decision = <-ch:
	case <-ctx.Done():
		return e.finishCanceled(runID, node)
	}

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

// retryConfig reads a node's retry settings: total attempts (1 + retries) and
// the base backoff between attempts. Defaults: 1 attempt, 200ms base backoff.
func retryConfig(node model.Node) (attempts int, backoff time.Duration) {
	attempts = 1
	if v := node.Field("retries"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			attempts = 1 + n
		}
	}
	backoff = 200 * time.Millisecond
	if v := node.Field("retry_backoff_ms"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			backoff = time.Duration(n) * time.Millisecond
		}
	}
	return attempts, backoff
}

// sleepCtx sleeps for d, returning false if the context is canceled first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

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
	switch outcome {
	case model.StatusFailed:
		return label == "failed"
	case model.StatusSkipped:
		// A filtered-out branch stops; no outgoing edge is taken.
		return false
	default:
		return label == "" || label == "success" || label == "approved"
	}
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
	case model.StatusSkipped:
		return model.StatusSkipped
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
