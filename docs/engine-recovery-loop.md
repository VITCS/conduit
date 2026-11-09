# Engine change: bounded Verify → Implement recovery loop

**Status:** implemented · **Scope owner:** engine

> Shipped: `Edge.MaxAttempts` + `Run.Loops`, validator back-edge rules, the
> scheduler's `tryRetry` / `computeLoopBodies`, non-sticky `failed`, an exec-count
> backstop, idempotent `github_pr`, verify feedback into `coding_apply`, the
> factory rewired to `apply → verify(commit) → pr` with a `verify → apply` retry
> (×2), and engine/validator tests. UI shows retry edges as `retry ×N`.

## Goal

When **Verify** (CI check-runs) fails, send the failing-check context back to the
**coding agent** for another attempt, bounded by a retry budget, instead of
routing straight to a human. Matches Superplane's *"a failed check returns to
implementation with actionable context."* Today the factory routes a CI failure
to **Needs attention** (human) — there is no automatic retry.

## Why the engine has to change (grounded in the code)

`internal/engine/engine.go` executes a workflow as a **concurrent DFS fan-out**
from trigger nodes, joined by a `WaitGroup`:

- `activate(id)` guards on a `started map[string]bool` — **every node runs at most
  once per run** (`internal/engine/engine.go:270`). A back-edge to an
  already-started node is a silent no-op.
- `Run.Steps` is keyed by `NodeID` — **one execution record per node**
  (`internal/model/model.go:117`). Re-running overwrites it.
- The validator **rejects all cycles** (`internal/validate/validate.go:76`,
  `findCycle`).
- The run's `final` status is sticky: **any** node ending `failed` sets
  `failed=true` and the whole run is `failed` (`engine.go:292`, `engine.go:331`).

So a loop needs four things the engine does not do today: (1) re-run a bounded set
of nodes, (2) track a per-loop attempt budget durably, (3) not mark the run failed
for an intermediate failure that is being retried, and (4) a validator that allows
*one specific kind* of back-edge.

## Design: the "retry back-edge" (bounded loop), not general cycles

Keep the run-once-per-activation core. Model the loop **explicitly** as a single
back-edge carrying a budget, rather than opening the engine to arbitrary cyclic
execution (which would complicate the join semantics and termination).

### 1. Data model

- `model.Edge` gains `MaxAttempts int \`json:"max_attempts,omitempty"\``. An edge
  with `MaxAttempts > 0` is a **retry back-edge**: it fires on a `failed` outcome
  and re-enters the loop body instead of terminating. It composes with the
  existing `Label:"failed"`.
- `model.Run` gains `Loops map[string]int \`json:"loops,omitempty"\`` keyed by
  `from\x00to`, tracking attempts so far. Persisted → survives restart (durable).

### 2. Loop body (computed statically, once per run)

For a retry edge `verify → apply`, the **loop body** is the set of nodes on every
path from the edge target (`apply`) to the edge source (`verify`), inclusive —
i.e. `reachable(apply) ∩ reaches(verify)`. Compute from the adjacency list at the
top of `runWorkflow`. For the factory that is `{apply, verify}` (see §6 on keeping
`github_pr` out of the body).

### 3. Scheduler change (`runWorkflow` goroutine, engine.go:287–304)

After `runNode` returns, when `outcome == StatusFailed`:

1. Look for a retry back-edge in `out[id]` whose budget remains
   (`Run.Loops[key] < edge.MaxAttempts`).
2. If found:
   - `MutateRun`: `Loops[key]++` (durable). Feedback is already in memory from the
     Verify node (`checks_summary`); also stash `verify_feedback`.
   - Under `mu`: for each node in the loop body, set `started[n]=false` and reset
     its step to `pending` (publish a `step.updated`). Publish a `log`:
     `"Verify failed — retry 2/3, sending checks back to the coding agent"`.
   - `activate(edge.To)` and **return** — do **not** set `failed=true`, do not
     follow the normal `failed` edges.
3. If none (no retry edge, or budget exhausted): set `failed=true` and follow the
   `failed` edges as today (→ Needs attention).

The `WaitGroup` already tracks the re-activated goroutines, so `wg.Wait()` blocks
until the loop resolves. All `started`/step mutations stay under the existing `mu`.

### 4. Run-final semantics

Set `failed=true` **only** when a `failed` outcome is routed terminally (budget
exhausted / no retry edge), not when it loops. A loop that later succeeds ends the
run `success`; a loop that exhausts its budget ends `failed` and routes to the
human status node (same as today).

### 5. Termination guarantees

- Per-edge budget `MaxAttempts` strictly bounds each loop.
- A global per-run **execution cap** (e.g. 500 node-runs) as a backstop against
  pathological multi-loop misconfigurations; exceeding it fails the run with a
  clear message.
- Validator bounds `MaxAttempts` (1–10).

### 6. External-effect idempotency (the real gotcha)

Re-running the loop body repeats side effects:

- `coding_apply` already **force-updates** the branch (`upsertBranch` →
  `PATCH …/git/refs`), so re-running is safe.
- `github_pr` is **not** idempotent — opening a PR for a head that already has one
  returns `422`. **Two fixes, do both:**
  1. **Keep `github_pr` out of the loop body.** Verify the *pushed commit's*
     check-runs (`github_checks` on `${memory.commit_sha}`) **before** opening the
     PR. Loop body becomes `{apply, verify}`; the PR opens once after checks pass.
     (Requires CI to run on branch push; note the trade-off if CI is PR-only.)
  2. Make `github_pr` idempotent anyway: `GET /pulls?head=owner:branch&base=…`;
     if one exists, return it as success instead of `POST`-ing. Good hygiene
     regardless of the loop.

### 7. Feed the failure back to the agent

`coding_apply`'s implement prompt should include the verify feedback when present,
e.g. append `"Previous attempt failed CI:\n${memory.checks_summary}\nFix those."`
so the retry is informed, not a blind re-run. One-line prompt change in
`internal/components/coding_agent.go` (implementChanges).

### 8. Validator change (`internal/validate/validate.go`)

- Exclude retry back-edges (`MaxAttempts > 0`) from `findCycle` so the one
  intentional back-edge is allowed.
- Add checks: a retry edge's target must be an **ancestor** of its source (a true
  back-edge, over the non-retry subgraph); `1 ≤ MaxAttempts ≤ 10`; the loop body
  must have a single entry (the edge target).

### 9. Step history

`Steps` keeps the **latest** attempt per node (overwrite), but every attempt is
already preserved in the **event stream** (`log` + `step.updated` events) and the
run log, so nothing is lost operationally. *Future enhancement (not in this
scope):* `StepRun.Attempts []StepAttempt` to show per-try records in the UI.

### 10. UI (small)

- Render a retry edge distinctly: label `retry ×N` (web edge rendering +
  `EDGE_LABELS`).
- The run rail already streams the retry `log` lines; optionally show an attempt
  badge on the looping node.

## Seed wiring (factory)

Reorder the tail to `apply → verify(commit) → [pass] pr → post`, and add the retry
back-edge `verify → apply` with `MaxAttempts: 2`, `Label:"failed"`. Budget
exhausted still routes `verify → fail` (Needs attention).

## Test plan (`internal/engine/engine_test.go`)

1. **Recovers:** `A → B(verify) → C`, retry edge `B→A` max 3, with `B` failing
   twice then succeeding. Assert: `A` ran 3×, `B` ran 3×, run `success`, `C` ran.
2. **Budget exhausted:** `B` always fails. Assert: exactly `max` attempts, run
   `failed`, routed to the `failed` edge, no hang.
3. **No infinite loop / global cap** honored.
4. **Validator:** retry back-edge accepted; a non-retry cycle still rejected; a
   retry edge to a non-ancestor rejected; `MaxAttempts` out of range rejected.
5. **Idempotent PR:** `github_pr` returns the existing PR on `422`/pre-existing.

## Risks

- **Concurrency:** re-activation must happen entirely under `mu`; the `started`
  reset + `activate` must not race a sibling branch. Mitigated by reusing the
  existing lock and the `WaitGroup`.
- **Non-idempotent effects:** addressed by §6; audit any component placed in a
  loop body for repeat-safety before shipping a loop that includes it.
- **Semantics change:** `failed` is no longer sticky per-node. Covered by test 1/2;
  existing non-loop workflows are unaffected (no retry edges → identical behavior).

## Rollout

Additive and backward-compatible: no `MaxAttempts` on any existing edge → engine
and validator behave exactly as today. Ship behind the seed change for the factory
only; other workflows opt in by adding a retry edge.
