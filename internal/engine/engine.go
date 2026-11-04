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
	"strings"
	"sync"
	"time"

	"conduit/internal/components"
	"conduit/internal/metrics"
	"conduit/internal/model"
	"conduit/internal/store"
)

// Engine runs workflows against a store.
type Engine struct {
	store store.Store

	mu          sync.Mutex
	approvals   map[string]chan string        // runID|nodeID -> decision channel
	cancels     map[string]context.CancelFunc // runID -> cancel (active runs only)
	canceledIDs map[string]bool               // runID -> canceled before/at claim time

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
		canceledIDs:  map[string]bool{},
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
		ProjectID:  wf.ProjectID,
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
	metrics.RunsStarted.Inc("")

	ctx, cancel := context.WithCancel(context.Background())
	e.mu.Lock()
	e.cancels[r.ID] = cancel
	e.mu.Unlock()

	go e.execute(ctx, r.ID, wf)
	return snap
}

// Enqueue creates a run in the queued state for a worker to claim and execute.
// This is the API/scheduler path in the worker model; Start keeps the immediate
// in-process path (used by tests).
func (e *Engine) Enqueue(wf model.Workflow, title string, input map[string]any) model.Run {
	if title == "" {
		title = wf.Name
	}
	r := &model.Run{
		ID:         nextRunID(),
		WorkflowID: wf.ID,
		ProjectID:  wf.ProjectID,
		Title:      title,
		Status:     model.StatusQueued,
		CreatedAt:  time.Now(),
		Steps:      map[string]*model.StepRun{},
		Memory:     map[string]any{},
		Input:      input,
	}
	for _, n := range wf.Nodes {
		r.Steps[n.ID] = &model.StepRun{NodeID: n.ID, Status: model.StatusPending}
	}
	snap, _ := e.store.CreateRun(r)
	metrics.RunsStarted.Inc("")
	return snap
}

// ExecuteClaimed runs a claimed run to completion. The worker calls this after
// ClaimRun; it loads the run's workflow and runs the DAG. parentCtx is the
// worker's lifetime; cancellation via Cancel(runID) is also honored.
func (e *Engine) ExecuteClaimed(parentCtx context.Context, run model.Run) {
	wf, ok, err := e.store.GetWorkflow(run.WorkflowID)
	if err != nil || !ok {
		e.store.MutateRun(run.ID, func(r *model.Run) {
			r.Status = model.StatusFailed
			now := time.Now()
			r.FinishedAt = &now
		})
		e.publish(model.Event{Type: "run.finished", RunID: run.ID, Status: model.StatusFailed,
			Level: "fail", Message: "workflow not found for run"})
		return
	}
	// If the run was canceled while queued (before any worker claimed it), don't
	// execute it — just finalize it as canceled.
	e.mu.Lock()
	if e.canceledIDs[run.ID] {
		delete(e.canceledIDs, run.ID)
		e.mu.Unlock()
		e.markCanceledFinished(run.ID)
		return
	}
	ctx, cancel := context.WithCancel(parentCtx)
	e.cancels[run.ID] = cancel
	e.mu.Unlock()
	e.execute(ctx, run.ID, wf)
}

// Cancel stops a run in any non-terminal state. If the run is actively executing
// it cancels its context (steps and approval waits unwind to canceled). If it is
// still queued (no worker has claimed it yet) it is marked canceled now, and a
// tombstone prevents a racing worker from executing it. Returns false only if the
// run is unknown or already in a terminal state.
func (e *Engine) Cancel(runID string) bool {
	e.mu.Lock()
	if cancel, ok := e.cancels[runID]; ok {
		e.mu.Unlock()
		cancel()
		return true
	}
	// Not actively executing here. Inspect the stored run.
	snap, ok, err := e.store.GetRun(runID)
	if err != nil || !ok {
		e.mu.Unlock()
		return false
	}
	switch snap.Status {
	case model.StatusQueued, model.StatusPending:
		// Queued (possibly claimable). Tombstone it so a worker that claims it
		// between now and ExecuteClaimed will skip execution, then finalize.
		e.canceledIDs[runID] = true
		e.mu.Unlock()
		e.markCanceledFinished(runID)
		return true
	case model.StatusRunning, model.StatusWaiting:
		// Active on another instance (no local cancel func). Best-effort: mark it
		// canceled in the shared store and notify; that instance stops renewing.
		e.canceledIDs[runID] = true
		e.mu.Unlock()
		e.markCanceledFinished(runID)
		return true
	default:
		e.mu.Unlock()
		return false // already success/failed/canceled/skipped
	}
}

// markCanceledFinished sets a run (and its unfinished steps) to canceled and
// publishes the terminal event, so the UI leaves the running state immediately.
func (e *Engine) markCanceledFinished(runID string) {
	snap, ok, _ := e.store.MutateRun(runID, func(r *model.Run) {
		if r.Status == model.StatusSuccess || r.Status == model.StatusFailed || r.Status == model.StatusCanceled {
			return
		}
		r.Status = model.StatusCanceled
		now := time.Now()
		r.FinishedAt = &now
		for _, st := range r.Steps {
			if st.Status == model.StatusPending || st.Status == model.StatusRunning || st.Status == model.StatusWaiting {
				st.Status = model.StatusCanceled
			}
		}
	})
	if !ok {
		return
	}
	e.publish(model.Event{Type: "run.finished", RunID: runID, Status: model.StatusCanceled,
		Level: "info", Message: fmt.Sprintf("Run %s canceled", runID), Run: &snap})
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

	metrics.RunsActive.Add(1)
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
	metrics.RunsActive.Add(-1)
	metrics.RunsFinished.Inc(metrics.Labels("status", string(final)))
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
		// Run the executor on a read-only snapshot WITHOUT holding the store lock,
		// so a connector doing network I/O never blocks the store (or deadlocks by
		// calling back into it). Memory changes come back via res.MemoryWrites.
		snap, _, _ := e.store.GetRun(runID)
		res = components.For(node.Type)(node, &snap)
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
	if len(res.MemoryWrites) > 0 {
		e.store.MutateRun(runID, func(r *model.Run) {
			if r.Memory == nil {
				r.Memory = map[string]any{}
			}
			for k, v := range res.MemoryWrites {
				r.Memory[k] = v
			}
		}) //nolint:errcheck // best-effort during execution
	}
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

	snap, _, _ := e.store.MutateRun(runID, func(r *model.Run) { r.Status = model.StatusWaiting })
	// Render the agent's suggestion (the node's templated summary) so the approver
	// sees what they're approving in the notification and banner, not just a title.
	summaryTmpl := node.Field("summary")
	if summaryTmpl == "" {
		summaryTmpl = node.Field("message")
	}
	detail := strings.TrimSpace(components.Render(&snap, summaryTmpl))
	e.setStep(runID, node.ID, model.StatusWaiting, func(s *model.StepRun) {
		s.Output = map[string]any{"approvers": node.Field("approvers")}
		if detail != "" {
			s.Output["summary"] = detail
		}
	})
	e.publish(model.Event{Type: "run.waiting", RunID: runID, NodeID: node.ID,
		Status: model.StatusWaiting, Level: "info",
		Message: fmt.Sprintf("%s · awaiting approval from %s", node.Title, node.Field("approvers")),
		Detail:  detail,
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
