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
	"conduit/internal/seed"
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

// EnqueueForTask queues a run bound to a board task, seeding the task's ticket
// context into run memory so the factory works on this task's issue (its fetch
// node can skip re-fetching). The run's TaskID links it back for stage projection.
func (e *Engine) EnqueueForTask(wf model.Workflow, t model.Task) model.Run {
	summary := t.Summary
	if summary == "" {
		summary = t.Title
	}
	r := &model.Run{
		ID:         nextRunID(),
		WorkflowID: wf.ID,
		ProjectID:  wf.ProjectID,
		Title:      "task: " + t.Title,
		Status:     model.StatusQueued,
		CreatedAt:  time.Now(),
		Steps:      map[string]*model.StepRun{},
		Memory: map[string]any{
			"ticket_key":         t.SourceRef,
			"ticket_summary":     summary,
			"ticket_description": t.Description,
			"ticket_url":         t.SourceURL,
			"ticket_source":      t.Source,
		},
		Input:  map[string]any{"task_id": t.ID},
		TaskID: t.ID,
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

// Rerun re-queues a finished run to execute again starting at nodeID. The node
// and everything downstream of it are reset to pending; successful upstream steps
// and accumulated run memory are kept, so a failed stage can be retried without
// redoing earlier work. The run must be in a terminal state (not queued/running/
// waiting). A worker then claims it and executes from nodeID (see execute).
func (e *Engine) Rerun(runID, nodeID string) (model.Run, error) {
	run, ok, err := e.store.GetRun(runID)
	if err != nil {
		return model.Run{}, err
	}
	if !ok {
		return model.Run{}, fmt.Errorf("run not found")
	}
	switch run.Status {
	case model.StatusQueued, model.StatusRunning, model.StatusWaiting, model.StatusPending:
		return model.Run{}, fmt.Errorf("run is still active; cancel it before re-running")
	}
	wf, ok, err := e.store.GetWorkflow(run.WorkflowID)
	if err != nil {
		return model.Run{}, err
	}
	if !ok {
		return model.Run{}, fmt.Errorf("workflow not found for run")
	}
	if _, found := nodeByID(wf, nodeID); !found {
		return model.Run{}, fmt.Errorf("node %q not in workflow", nodeID)
	}

	reset := descendants(wf, nodeID) // nodeID plus everything reachable from it
	now := time.Now()
	snap, _, err := e.store.MutateRun(runID, func(r *model.Run) {
		for nid := range reset {
			r.Steps[nid] = &model.StepRun{NodeID: nid, Status: model.StatusPending}
		}
		r.Loops = nil // fresh recovery-loop budget for the retried region
		r.FinishedAt = nil
		r.Status = model.StatusQueued
		r.ResumeFrom = []string{nodeID}
	})
	if err != nil {
		return model.Run{}, err
	}
	metrics.RunsStarted.Inc("")
	e.publish(model.Event{Type: "run.queued", RunID: runID, Status: model.StatusQueued,
		Level: "info", Message: fmt.Sprintf("Re-running from %s", nodeID), Time: now, Run: &snap})
	return snap, nil
}

// nodeByID returns the workflow node with the given id.
func nodeByID(wf model.Workflow, id string) (model.Node, bool) {
	for _, n := range wf.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return model.Node{}, false
}

// descendants returns the set containing start and every node reachable from it by
// following outgoing edges (all labels, retry back-edges included). A re-run resets
// exactly this set so stale downstream results don't linger.
func descendants(wf model.Workflow, start string) map[string]bool {
	out := map[string][]string{}
	for _, ed := range wf.Edges {
		if ed.MaxAttempts > 0 {
			continue // skip retry back-edges so we don't reset upstream nodes
		}
		out[ed.From] = append(out[ed.From], ed.To)
	}
	seen := map[string]bool{start: true}
	queue := []string{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, to := range out[cur] {
			if !seen[to] {
				seen[to] = true
				queue = append(queue, to)
			}
		}
	}
	return seen
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
	// Bounded recovery loops: for each retry back-edge, the set of nodes to reset
	// and re-run on a retry (the nodes on a path from the edge target back to its
	// source). Keyed "from\x00to" to match Run.Loops.
	loopBodies := computeLoopBodies(wf)

	metrics.RunsActive.Add(1)
	e.publish(model.Event{Type: "run.started", RunID: runID, Level: "info",
		Message: fmt.Sprintf("Run %s started", runID)})

	// Parallel scheduler: a node is activated by the first incoming edge whose
	// label matches its predecessor's outcome, and runs exactly once. Independent
	// branches run concurrently in their own goroutines. (A true join — waiting
	// for all predecessors — is a later addition; "merge" is a pass-through today.)
	var (
		mu        sync.Mutex
		started   = map[string]bool{}
		wg        sync.WaitGroup
		failed    bool
		canceled  bool
		execCount int
	)
	const execCap = 500 // backstop against a pathological loop misconfiguration

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

			mu.Lock()
			execCount++
			over := execCount > execCap
			if over {
				failed = true
			}
			mu.Unlock()
			if over {
				e.publish(model.Event{Type: "log", RunID: runID, NodeID: id, Level: "fail",
					Message: fmt.Sprintf("execution cap (%d) exceeded — stopping to prevent a runaway loop", execCap)})
				return
			}

			outcome := e.runNode(ctx, runID, node)
			// Stop this branch if the run was canceled — either the node itself
			// unwound to canceled, or cancellation arrived while its executor was
			// mid-call (components run network I/O that doesn't observe ctx, so they
			// can return failed/success after a cancel). Without this ctx check a
			// failing node with a retry back-edge (e.g. CI) would loop back forever
			// after Cancel, never letting the run finalize.
			if outcome == model.StatusCanceled || ctx.Err() != nil {
				mu.Lock()
				canceled = true
				mu.Unlock()
				return // stop propagating this branch (no retry, no successors)
			}

			// Bounded recovery loop: a failing node with a retry back-edge that still
			// has budget re-enters the loop body (carrying its failure context in
			// memory) instead of being treated as a terminal failure.
			if outcome == model.StatusFailed && e.tryRetry(runID, node, out[id], loopBodies, &mu, started, activate) {
				return
			}
			if outcome == model.StatusFailed {
				mu.Lock()
				failed = true
				mu.Unlock()
			}
			for _, ed := range out[id] {
				if ed.MaxAttempts > 0 {
					continue // retry back-edges are handled by tryRetry
				}
				if edgeMatches(ed.Label, outcome) {
					activate(ed.To)
				}
			}
		}()
	}

	// Normally a run begins at its trigger nodes. A re-run (ResumeFrom set) instead
	// begins at the chosen node(s), so the already-successful upstream steps are
	// kept and only the failed stage and everything after it re-execute. ResumeFrom
	// is consumed here so a later normal finish doesn't repeat it.
	startNodes := []string{}
	if snap, ok, _ := e.store.GetRun(runID); ok && len(snap.ResumeFrom) > 0 {
		startNodes = append(startNodes, snap.ResumeFrom...)
		e.store.MutateRun(runID, func(r *model.Run) { r.ResumeFrom = nil }) //nolint:errcheck
	} else {
		for _, n := range wf.Nodes {
			if n.Kind == model.KindTrigger {
				startNodes = append(startNodes, n.ID)
			}
		}
	}
	for _, id := range startNodes {
		activate(id)
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
	e.finalizeTask(&snap, final)
	e.publish(model.Event{Type: "run.finished", RunID: runID, Status: final,
		Level: levelFor(final), Message: fmt.Sprintf("Run %s %s", runID, final),
		Run: &snap})
}

// projectTaskStage advances a bound task's board stage as its run enters a node
// that declares a Stage. No-op for runs that aren't bound to a task.
func (e *Engine) projectTaskStage(runID string, node model.Node) {
	if node.Stage == "" {
		return
	}
	run, ok, _ := e.store.GetRun(runID)
	if !ok || run.TaskID == "" {
		return
	}
	stage := model.TaskStage(node.Stage)
	snap, ok, _ := e.store.MutateTask(run.TaskID, func(t *model.Task) {
		t.CurrentRunID = runID
		t.SetStage(stage, time.Now())
	})
	if ok {
		e.publish(model.Event{Type: "task.updated", RunID: runID, Task: &snap, Time: time.Now()})
	}
}

// finalizeTask projects the run's terminal outcome onto its task (Done on success,
// Blocked otherwise) and copies the produced artifacts (PR, branch, confidence).
func (e *Engine) finalizeTask(run *model.Run, final model.Status) {
	if run == nil || run.TaskID == "" {
		return
	}
	stage := model.StageDone
	if final != model.StatusSuccess {
		stage = model.StageBlocked
	}
	snap, ok, _ := e.store.MutateTask(run.TaskID, func(t *model.Task) {
		t.SetStage(stage, time.Now())
		if v, ok := run.Memory["last_pr_url"].(string); ok && v != "" {
			t.PRURL = v
		}
		if v, ok := run.Memory["work_branch"].(string); ok && v != "" {
			t.Branch = v
		}
		if c := memInt(run.Memory["confidence_score"]); c > 0 {
			t.Confidence = c
		}
	})
	if ok {
		e.publish(model.Event{Type: "task.updated", RunID: run.ID, Task: &snap, Time: time.Now()})
	}
}

func memInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

// tryRetry handles a failing node's bounded recovery loop. If one of its outgoing
// edges is a retry back-edge with budget remaining, it bumps the durable attempt
// counter, resets the loop body (started flags + step records) and re-activates the
// loop entry, returning true. The failure context the node wrote to memory (e.g.
// checks_summary) is already in the run, so the re-run is informed. Returns false
// when there is no retry edge or its budget is exhausted — the caller then routes
// the failure normally.
func (e *Engine) tryRetry(runID string, node model.Node, edges []model.Edge, loopBodies map[string][]string, mu *sync.Mutex, started map[string]bool, activate func(string)) bool {
	for _, ed := range edges {
		if ed.MaxAttempts <= 0 || ed.Label != "failed" {
			continue
		}
		k := ed.From + "\x00" + ed.To
		attempts := -1
		e.store.MutateRun(runID, func(r *model.Run) {
			if r.Loops == nil {
				r.Loops = map[string]int{}
			}
			if r.Loops[k] < ed.MaxAttempts {
				r.Loops[k]++
				attempts = r.Loops[k]
			}
		}) //nolint:errcheck
		if attempts < 0 {
			continue // budget exhausted — let the normal failed edge handle it
		}
		body := loopBodies[k]
		mu.Lock()
		for _, nid := range body {
			started[nid] = false
		}
		mu.Unlock()
		e.store.MutateRun(runID, func(r *model.Run) {
			for _, nid := range body {
				if st := r.Steps[nid]; st != nil {
					st.Status = model.StatusPending
					st.StartedAt = nil
					st.EndedAt = nil
				}
			}
		}) //nolint:errcheck
		e.publish(model.Event{Type: "log", RunID: runID, NodeID: node.ID, Level: "run",
			Message: fmt.Sprintf("%s · failed — retry %d/%d, re-running from %q with feedback", node.Title, attempts, ed.MaxAttempts, ed.To)})
		activate(ed.To)
		return true
	}
	return false
}

// computeLoopBodies maps each retry back-edge ("from\x00to") to the nodes on a
// path from its target back to its source over the normal (non-retry) edges.
func computeLoopBodies(wf model.Workflow) map[string][]string {
	fwd := map[string][]string{}
	bwd := map[string][]string{}
	for _, ed := range wf.Edges {
		if ed.MaxAttempts > 0 {
			continue
		}
		fwd[ed.From] = append(fwd[ed.From], ed.To)
		bwd[ed.To] = append(bwd[ed.To], ed.From)
	}
	bodies := map[string][]string{}
	for _, ed := range wf.Edges {
		if ed.MaxAttempts <= 0 {
			continue
		}
		forward := reachSet(fwd, ed.To)    // reachable from the loop entry
		backward := reachSet(bwd, ed.From) // can reach the loop-back node
		var body []string
		for n := range forward {
			if backward[n] {
				body = append(body, n)
			}
		}
		bodies[ed.From+"\x00"+ed.To] = body
	}
	return bodies
}

func reachSet(adj map[string][]string, start string) map[string]bool {
	seen := map[string]bool{start: true}
	stack := []string{start}
	for len(stack) > 0 {
		u := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, v := range adj[u] {
			if !seen[v] {
				seen[v] = true
				stack = append(stack, v)
			}
		}
	}
	return seen
}

// runNode executes one node and returns its outcome. It retries a failed
// executor up to the node's configured attempts, and returns StatusCanceled if
// the run's context is canceled. Approval nodes block until Resolve or cancel.
func (e *Engine) runNode(ctx context.Context, runID string, node model.Node) model.Status {
	// A disabled step is bypassed: its executor is not called, it is marked skipped,
	// and the run proceeds down its success edges. Triggers are never bypassed.
	if node.Disabled && node.Kind != model.KindTrigger {
		return e.bypassNode(runID, node)
	}
	start := time.Now()
	e.setStep(runID, node.ID, model.StatusRunning, func(s *model.StepRun) {
		s.StartedAt = &start
	})
	// Broadcast the running transition as a step update (not just a log) so the UI
	// shows "In progress" live, without a page refresh.
	e.publish(model.Event{Type: "step.updated", RunID: runID, NodeID: node.ID,
		Status: model.StatusRunning, Level: "run", Step: e.stepSnap(runID, node.ID),
		Message: fmt.Sprintf("%s · running", node.Title)})
	e.publish(model.Event{Type: "log", RunID: runID, NodeID: node.ID, Level: "run",
		Message: fmt.Sprintf("%s · running", node.Title)})
	e.projectTaskStage(runID, node)

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

	// When a step opens a real pull request, hand it off to the GitHub review
	// workflow's inbox so a reviewer can trigger the review (see maybeHandoffPR).
	if node.Type == "github_pr" && (res.Outcome == model.StatusCreated || res.Outcome == model.StatusSuccess) {
		e.maybeHandoffPR(runID, node, res.Output)
	}
	return res.Outcome
}

// maybeHandoffPR records a review request for a freshly opened PR, so it appears in
// the GitHub project's review inbox. It is a no-op when the run is already in the
// GitHub project (no self-review), when the review workflow is absent, or when the
// output carries no PR number.
func (e *Engine) maybeHandoffPR(runID string, node model.Node, out map[string]any) {
	run, ok, _ := e.store.GetRun(runID)
	if !ok || run.ProjectID == seed.GitHubProjectID {
		return
	}
	wf, ok, _ := e.store.GetWorkflow(seed.GitHubWorkflowID)
	if !ok {
		return
	}
	number := toInt(out["number"])
	if number <= 0 {
		return
	}
	owner, repo := splitRepo(fmt.Sprintf("%v", out["repo"]))
	if owner == "" || repo == "" {
		return
	}
	title := strings.TrimSpace(components.Render(&run, node.Field("title")))
	if title == "" {
		title = fmt.Sprintf("PR #%d", number)
	}
	source := run.Title
	if wfSrc, ok, _ := e.store.GetWorkflow(run.WorkflowID); ok && wfSrc.Name != "" {
		source = wfSrc.Name
	}
	rr, err := e.store.CreateReviewRequest(model.ReviewRequest{
		ID:         fmt.Sprintf("rr_%d", time.Now().UnixNano()),
		ProjectID:  wf.ProjectID,
		WorkflowID: wf.ID,
		Owner:      owner, Repo: repo, Number: number,
		URL:    fmt.Sprintf("%v", out["html_url"]),
		Head:   fmt.Sprintf("%v", out["head"]),
		Title:  title,
		Source: source, SourceRun: runID, Status: "pending",
	})
	if err != nil {
		return
	}
	msg := fmt.Sprintf("Opened PR #%d in %s/%s — sent to the GitHub review inbox. Open the GitHub project to run the review.", number, owner, repo)
	// On the source run's stream (the user is watching this run), and tagged with
	// the target project so a project-wide listener can surface it too.
	e.publish(model.Event{Type: "review.requested", RunID: runID, ProjectID: wf.ProjectID,
		Level: "info", Message: msg, Review: &rr, Time: time.Now()})
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

func splitRepo(s string) (owner, repo string) {
	if i := strings.IndexByte(s, '/'); i > 0 {
		return s[:i], s[i+1:]
	}
	return "", ""
}

// bypassNode records a disabled step as skipped and returns success so the run
// continues down the node's success edges (as if the step had passed).
func (e *Engine) bypassNode(runID string, node model.Node) model.Status {
	now := time.Now()
	e.setStep(runID, node.ID, model.StatusSkipped, func(s *model.StepRun) {
		s.StartedAt = &now
		s.EndedAt = &now
		s.Output = map[string]any{"bypassed": true}
		s.Logs = []string{"bypassed — node disabled"}
	})
	e.publish(model.Event{Type: "step.updated", RunID: runID, NodeID: node.ID,
		Status: model.StatusSkipped, Level: "info", Step: e.stepSnap(runID, node.ID),
		Message: node.Title + " · bypassed (disabled)"})
	return model.StatusSuccess
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
