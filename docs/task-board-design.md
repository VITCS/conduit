# Task board: Backlog → Implement → Verify → Done

**Status:** Phase 1 implemented · **Scope owner:** full-stack · **Est:** full ~1.5–2 weeks

> Phase 1 shipped: `model.Task` + `Run.TaskID` + `Node.Stage`; Store task methods
> (mem + postgres `tasks` table); the engine stage-projection hook
> (`projectTaskStage` / `finalizeTask`) + `EnqueueForTask`; `task.*` SSE events;
> the tasks API (`/api/tasks` CRUD + `/start` + `/stage`, `/api/intake/{project}`);
> factory `Node.Stage` wiring + a fetch that prefers the task's ticket; and the
> Board UI (Editor/Board toggle, five live columns, New task, Start, View run, PR).
> Phase 2 shipped: token-authed `/api/intake/{project}?source=` webhooks with
> GitHub / Jira / generic (Notion) payload normalization; heuristic
> confidence-at-intake + an auto-start policy (`auto_start_confidence` project var);
> a task detail drawer (description, confidence, run history, stage move, delete);
> and board search + source filter. Phase 3 (a factory-performance metrics page,
> multiple workflows per board, task comments) remains.

## Goal

Add Superplane's **board**: a persistent **task** that moves across
**Backlog → Implement → Verify → Done** (plus **Blocked**) columns, accumulating
context (source issue, runs, PR, checks, approvals, notes) across executions. It is
the "operate" view; the canvas stays the "author" view.

## The core idea: a task is a projection over runs, not a replacement

Conduit already has the execution half: **Workflow** (the automation graph) and
**Run** (one execution, with steps/memory/approvals — `model.Run`). What's missing
is the durable **unit of work** that spans runs and carries board state.

- **Task** = the durable unit (one per issue). Holds source, stage, confidence,
  current run, artifacts, history.
- **Run** = one execution of the factory workflow *for* a task (unchanged).
- **Workflow** = the automation (unchanged — the factory graph we already have).
- **Stage** = a **projection of run progress**, not a separate workflow per column.
  As a task's run crosses stage-boundary nodes, the engine updates `task.stage`.

This reuses everything already built — approvals, triage/confidence, Verify (CI),
the recovery loop — instead of reworking the engine into per-column automations.

## Data model

```go
// model/task.go
type TaskStage string // backlog | implement | verify | done | blocked
type Task struct {
    ID, ProjectID, WorkflowID string
    Title        string
    Source       string // jira | notion | github | manual
    SourceRef    string // e.g. "ENG-482"
    SourceURL    string
    Summary      string
    Description  string
    Stage        TaskStage
    Confidence   int    // 0–100, from triage at intake
    CurrentRunID string // the latest/active run
    Branch       string
    PRURL        string
    CreatedAt, UpdatedAt, StageChangedAt time.Time
}
```

- `model.Run` gains `TaskID string \`json:"task_id,omitempty"\`` to link a run back
  to its task.
- `model.Node` gains `Stage string \`json:"stage,omitempty"\`` — the stage a node
  belongs to (empty = no stage change). The engine projects it onto the task.

## Store (interface + mem + postgres)

Add to `store.Store` (mirrors the existing Run methods):

```go
CreateTask(t model.Task) (model.Task, error)
GetTask(id string) (model.Task, bool, error)
ListTasks(projectID string, stage model.TaskStage) ([]model.Task, error) // stage "" = all
MutateTask(id string, fn func(*model.Task)) (model.Task, bool, error)     // atomic update
DeleteTask(id string) error
ListRunsByTask(taskID string) ([]model.Run, error)                       // task history
```

- `internal/store/store.go` (mem): a `tasks map[string]*model.Task` + mutex, same
  pattern as runs.
- `internal/store/postgres.go`: a `tasks` table (additive migration — id, project_id,
  workflow_id, title, source, source_ref, source_url, summary, description, stage,
  confidence, current_run_id, branch, pr_url, timestamps; index on
  `(project_id, stage)` and on `runs(task_id)`).

## Engine: stage projection hook

One small hook, no scheduler rewrite. In `runNode`/run-finish, when `run.TaskID != ""`:

- On a step entering a node with `Stage != ""`: `MutateTask` → set `Stage`,
  `StageChangedAt`, publish `task.updated`.
- On run finish: terminal projection — `success` → `done`; `failed`/routed to a
  Blocked status node → `blocked`. Copy artifacts from run memory
  (`last_pr_url` → `PRURL`, `work_branch` → `Branch`, `confidence_score` →
  `Confidence`).

Factory `node.Stage` mapping (seed): `triage`→(gate, no change), `plan`/`impl`/
`cplan`/`apply`→`implement`, `vchk`→`verify`, `pr`/`post`/`mem`→`done`,
`clar`/`fail`→`blocked`.

## Intake (task creation) — the authorization boundary

Superplane: discovery runs continuously; **Start** is the control point where a
task enters the line.

- **Manual:** `POST /api/tasks` creates a task in **Backlog**.
- **Automatic:** `POST /api/intake/{project}` webhook accepts an issue payload
  (Jira/Notion/GitHub), creates a Backlog task, and runs **triage** to set
  `Confidence`. High confidence can auto-Start (configurable); otherwise it waits
  in Backlog for a human.
- **Start:** `POST /api/tasks/{id}/start` → `Engine.Enqueue(factoryWF, title,
  input)` with the task's `summary`/`description` seeded as the run input and
  `run.TaskID` set; task → **Implement**. This is the reused run path
  (`api.go:839` pattern).

## API

```
GET    /api/tasks?project=&stage=      list (board columns)
POST   /api/tasks                      manual create → Backlog
GET    /api/tasks/{id}                 detail (+ ListRunsByTask history)
POST   /api/tasks/{id}/start           enqueue the bound run
POST   /api/tasks/{id}/stage           manual override / archive (audited)
POST   /api/intake/{project}           webhook intake (+ optional auto-start)
```

Live board over the existing SSE stream (`/api/events`, `store.Broker`): new
`task.created` / `task.updated` event types; the board subscribes exactly like the
run view does today.

## UI

- A top-level **Board / Editor** toggle. Board = columns **Backlog · Implement ·
  Verify · Done · Blocked**.
- **Card:** title, source chip (Jira/Notion/GitHub), confidence meter, PR link,
  a *needs-attention* badge (blocked or waiting on approval), and a primary action
  — **Start** (Backlog) or **Approve** (when its run is waiting).
- **Detail drawer:** task context, the current run rendered with the *existing*
  canvas/run view (reuse), run history (prior runs by `TaskID`), artifacts.
- Live updates from `task.*` events. The canvas (author view) is unchanged.

## Phasing

- **Phase 1 — MVP (~3–4 days):** Task model + store (mem+pg) + `Run.TaskID` +
  `Node.Stage` + engine stage hook + API (list/create/get/start/stage) + Board UI
  (read, Start, Approve, live) + manual intake + seed stages on the factory.
- **Phase 2 (~3–4 days):** automatic intake webhooks (Jira/Notion/GitHub → task),
  confidence-at-intake routing + auto-start policy, task detail history + notes,
  per-stage time metrics, board filters/search.
- **Phase 3:** a **factory-performance** page (time-in-stage, retry rate,
  intervention rate — Superplane's "Done is operating data"), multiple workflows
  per board, task comments.

## Risks & decisions

- **Single source of truth:** the **run** is authoritative for execution;
  `task.stage` is a projection written **only** by the engine hook. Manual stage
  changes are explicit and audited (`AppendAudit`), never silent.
- **Multiple runs per task:** `CurrentRunID` points at the active run; history is
  `ListRunsByTask`. Re-running a whole task starts a new run bound to the same task.
- **Concurrency:** `MutateTask` is atomic (same pattern as `MutateRun`).
- **Postgres migration:** additive table + indexes; mem store mirrors it.
- **Scope creep:** Phase 1 is strictly read + Start + Approve + live; no drag-drop,
  no metrics, no auto-intake. Those are Phase 2/3.
- **Backward compatible:** tasks are opt-in. Workflows with no `Node.Stage` and
  runs with no `TaskID` behave exactly as today; the board is simply empty until a
  task is created.

## What this explicitly reuses (not rebuilt)

Approvals + the rich approval summary, triage/confidence routing, Verify (CI
check-runs), the bounded recovery loop, the run engine/queue/SSE, projects/roles,
secrets/variables, and the canvas run visualization. The board is a thin durable
layer and an operate view on top of all of it.
