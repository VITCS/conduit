# Conduit Agent Roadmap

Conduit Agent is an open-source AI software factory: a ticket goes in, agents plan
and write the change under human approval, CI verifies it, and a reviewed pull
request comes out. This file records how it got here, phase by phase, and what
comes next.

The phases below built the foundation first (a workflow engine with approval
gates, integrations, multi-tenancy, scale), then the AI Factory on top of it. The
engine idea itself, typed components on a graph with human approval gates, was
inspired by workflow control planes such as SuperPlane. Conduit is its own
codebase and product.

## Where we are (shipped)

- Workflow engine: DAG executor, `success`/`failed`/`approved` edge routing,
  human approval gates, `skipped` propagation.
- Store interface with in-memory + Postgres, JSONB, row-locked mutations.
- REST + SSE API, save-time validation (cycles, dangling edges, …).
- Next.js 16 / React Flow UI (canvas editor, run rail, inspector, console,
  memory), embedded into a single Go binary.
- One-command Docker (`make up`), CI, test suite, docs.
- The AI Factory: ticket intake and board, triage, planning and coding agents,
  two human approval gates, CI verification, pull request and review handoff,
  per-user workspaces (Phase 6).

## Platform scope

The longer-term scope of the engine and platform, grouped by theme. Much of it has
shipped (see the phases below); the rest is open, and is listed under
[What's next](#whats-next) where it is actively planned.

| Area | Target |
|------|--------|
| Components | `http`, `graphql`, `ssh`, `if`, `filter`, `foreach`, `loop`, `merge`, `wait`, `timegate`, `approval`, `runner`, `factory`, `noop`, memory (`read`/`write`/`upsert`/`add`/`delete`) |
| Triggers | `webhook`, `schedule` (cron), `start`, `messages`, `onerror` |
| Execution | retries + backoff, cancellation, timeouts, parallel branches, sub-runs |
| Integrations | GitHub, GitLab, AWS, GCP, Azure, DigitalOcean, Datadog, Slack, Discord, Linear, Jira, … behind a connector + secret-provider interface |
| Secrets | encrypted secret store, per-connector credentials |
| Multi-tenancy | organizations → projects → workflows; members & invitations |
| AuthZ/AuthN | login (OIDC), API tokens, RBAC (roles/permissions) |
| Realtime/scale | Postgres `LISTEN/NOTIFY` event bus, multiple instances, run queue/workers |
| Agents | triage, planning and review agents, a coding agent that pushes real branches, CI verification, PR review — the AI Factory |
| Ops | metrics (Prometheus/OTel), structured logging, audit log, rate limits |

---

## Phases

### Phase 1 — Execution robustness & inbound triggers  ✅ (this release)

The engine becomes dependable and reachable from the outside.

- **Retries with backoff** — per-node `retries` + `retry_backoff_ms`; the engine
  re-runs a failed executor before taking the `failed` edge.
- **Run cancellation** — `POST /api/runs/{id}/cancel`; context-aware engine that
  stops a running or waiting run and marks it `canceled`.
- **Webhook trigger** — `POST /api/hooks/{workflowId}` starts a run and binds the
  posted JSON as the trigger's output (`input`), usable downstream.
- **Real HTTP executor** — opt-in (`live: true`) node that performs an actual
  HTTP request with method/headers/body/timeout; outcome from the status code.
  The simulated default is preserved so the demo workflow still runs offline.
- UI: `Cancel` action, `Canceled` status, run-input surfaced in the inspector.

### Phase 2 — Scheduling, parallelism & run history  ✅ (this release)

- **Parallel branches**: independent DAG branches run concurrently (goroutine per
  activated node), replacing the sequential BFS; cancellation/retries/approval
  gating preserved.
- **Schedule trigger** (cron): a background scheduler (`internal/scheduler`,
  robfig/cron) starts runs from `schedule` trigger nodes; re-syncs on save and
  every 30s; `GET /api/schedules` lists active schedules.
- **Run history UI**: the rail toggles to a list of past runs; pick one to load
  its steps, outputs and memory.
- New components: `filter` (stops a branch via a `skipped` outcome, no failure),
  `merge` (pass-through join), `graphql`, plus `schedule`/`start` triggers.

Still deferred to later phases: bounded worker pool / run queue, per-node
timeouts, true `foreach`/`loop` and join semantics for `merge`, run filtering &
re-run in the UI.

### Phase 3 — Integrations & secrets  ✅ (this release)

- **Secret store**: AES-256-GCM encrypted-at-rest secrets (`internal/secrets`)
  over the store (Mem + Postgres); values are write-only and never returned.
  Key from `CONDUIT_SECRET_KEY` (ephemeral in dev).
- **Secret resolution**: components expand `${secret:NAME}` in their config, so
  credentials never live in the workflow graph.
- **Connectors**: a real `slack` component (posts to a webhook) and a
  secret-aware **live HTTP** executor (auth header, body) performing real side
  effects. Executors now run OFF the store lock so network I/O can't block it.
- **API + UI**: `GET/PUT/DELETE /api/secrets`, plus a Secrets management panel.

Deferred: a broader connector framework with per-provider setup/OAuth and more
integrations (GitHub API, Datadog, …); SSRF guards on outbound requests (Phase 7).

### Phase 4a — Authentication & RBAC  ✅ (this release)

- **AuthN**: password login (bcrypt) issuing stateless HS256 **JWTs**; a bootstrap
  admin is created on first run (`CONDUIT_ADMIN_EMAIL`/`PASSWORD`).
- **API tokens**: random, stored only as SHA-256 hashes; used for CI and to
  authenticate **webhooks** (`?token=` or bearer).
- **RBAC**: roles (owner/admin/member/viewer); write methods require a non-viewer
  role; auth middleware protects every `/api` route (health, login and the static
  UI stay public).
- **UI**: a login screen, user chip, and sign-out.

### Phase 4b — Multi-tenancy (projects)  ✅ (this release)

- **Projects** are a tenancy boundary: workflows and runs carry a `project_id`
  and every list/get/create is scoped to the selected project (via the
  `X-Conduit-Project` header). Cross-project access returns 404.
- Project CRUD (`/api/projects`) and a **project switcher** in the UI; a fresh
  project gets a usable starter workflow.
- A `default` project is seeded on first run and holds the sample workflow.

### Phase 4c — Per-project membership  ✅ (this release)

- **Project membership** with per-project roles: a user may access a project only
  if they are a member (global owner/admin override); the member role gates writes
  within that project. Non-members get 403 — true isolation by *user*.
- **Members & users API/UI**: list/add/remove project members, create users (admin).
- **Project-scoped secrets**: secrets live under a project and resolve per-run by
  the run's project; `${secret:NAME}` is isolated across projects.
- The project creator becomes its owner; the bootstrap admin owns the default project.

Deferred to later: invitations/email, OIDC / SSO, a per-project secret UI scope
indicator, and many-workflows-per-project navigation.

### Phase 5 — Realtime, scale & ops  ✅ (this release)

- **Postgres `LISTEN/NOTIFY`** event bus: run events fan out across every server
  instance, so SSE works behind a load balancer (enabled automatically with a DB).
- **Observability**: Prometheus `/metrics` (HTTP + runs), structured JSON logs
  with per-request IDs, and an **audit log** (`/api/audit`, admin) of mutations.
- **Rate limiting**: per-IP token bucket on `/api` (`CONDUIT_RATE_LIMIT`), plus
  the existing request-body size limits.

### Phase 5b — Worker model  ✅ (this release)

- **Durable run queue**: `POST /api/runs`, webhooks, and schedules now *enqueue*
  runs (`queued`) instead of running them inline.
- **Claim/lease workers**: a worker pool drains the queue; each run is claimed with
  a lease (Postgres `SELECT … FOR UPDATE SKIP LOCKED`), so across every instance
  exactly one worker executes a given run. The lease is renewed while a run
  executes or waits on approval.
- **Crash recovery**: if a worker dies, its lease expires and another worker
  reclaims the run. `CONDUIT_WORKERS` sets the pool size.

Deferred: OTel traces, and resumable execution so a reclaimed run continues from
its suspension point rather than re-running (today a reclaim re-executes; fine for
idempotent/simulated steps). Cross-instance approval resolution needs that
resumable state machine and is tracked with it.

### Phase 6 — The AI Factory  ✅

The first release modelled the "ticket → reviewed PR" flow with simulated agents.
It is now a real pipeline that opens real pull requests:

- **Model adapters**: the `agent` component calls **Anthropic**, **OpenAI** or a
  **custom** gateway with a project-scoped secret; provider and model are project
  settings. Prompt templating (`${input.…}`, `${memory.…}`, `${var:…}`,
  `${secret:…}`) gives agents run context.
- **Ticket intake**: Jira and Notion source nodes, a webhook intake
  (`/api/intake/{project}`) for GitHub issues and Jira tickets, and a **board**
  (Backlog → Implement → Verify → Done / Blocked) with optional auto-start above a
  confidence threshold.
- **Triage**: scores a ticket's readiness and routes vague work to "needs
  clarification" instead of an agent.
- **Coding agent**: `coding_plan` proposes the exact file changes; after a second
  human approval, `coding_apply` writes each file and pushes a branch through the
  GitHub Git Data API (no local checkout), with truncation detection.
- **CI verification**: `github_checks` waits for the commit's check-runs and loops
  back to the coding agent with the failures, up to twice.
- **Pull request + review handoff**: `github_pr` opens the PR, posts it back to the
  ticket, and sends it to the GitHub review workflow's inbox (agent review →
  security scan → human approval → merge), which a person starts.
- **Claude Code engine**: per project, Plan changes and Apply changes can run a real
  Claude Code session in a fresh checkout (reads the whole repo, edits files, runs
  allowed commands such as the tests), signed in with the user's Claude subscription
  token or an Anthropic API key. Conduit pushes only after verifying that just the
  approved files changed. The direct-API engine remains the default.
- **Diff review before push**: Apply Changes prepares the change without pushing;
  a Review Diff gate shows the actual code (file by file) and Push Branch commits
  exactly what was approved. Works with both coding engines.
- **Send back with feedback** at every approval gate: the proposal goes back to the
  step that made it with the reviewer's note (bounded to 3 per gate).
- **Project context**: a per-project brief (stack, commands, conventions,
  off-limits) added to every agent's instructions, plus the repo's CLAUDE.md /
  AGENTS.md for the direct-API coding engine.
- **Run control**: skip any step with a checkbox on its node, re-run a failed run
  from the stage that failed, cancel at any point, and watch status live.
- **Per-user workspaces**: everyone who signs in gets their own copy of the
  AI Factory and review workflows, with settings and secrets encrypted at rest.
- **Model tiers and cost tracking**: fast / standard / best models per project,
  a default tier per step, automatic step-up on low confidence, large changes,
  failed CI or a send-back (logged with the reason), and tokens and cost per step,
  per run and per model in Metrics.

### Phase 7 — Release hardening  ✅ (this release)

- **SSRF guard** on all outbound executors (`http` live, `slack`, `agent`):
  private/loopback/link-local targets are blocked at dial time; opt in with
  `CONDUIT_ALLOW_PRIVATE_EGRESS`.
- **Input/transport hardening**: 4 MiB request-body cap, security headers
  (`nosniff` / `SAMEORIGIN` / `no-referrer`), on top of existing rate limiting,
  timeouts, and graceful shutdown.
- **SECURITY.md**: security model, production checklist, and tracked limitations.
- Tagged **`v1.0.0`**.

---

## What's next

Planned, not shipped. Order is a rough priority.

- **Lower model spend per run** — model tiers and usage tracking have shipped
  (see Phase 6); next: prompt caching for the per-file coding calls, merging the
  three pre-approval agent calls, and a per-run spending cap.
- **More coding agents and places to run them** — the Claude Code engine has
  shipped (see Phase 6); next: a per-session container sandbox for it, a local
  runner on a developer's machine, and handing the coding step to tools an
  organization already licenses (for example the GitHub Copilot coding agent),
  while Conduit keeps triage, approvals, CI and review.
- **More model backends** — AWS Bedrock, Google Vertex AI and Azure, so teams can
  use an existing cloud agreement instead of a separate API key.
- **Resumable execution** — a reclaimed run resumes instead of restarting, which
  also enables approving from any instance.
- **More sources and targets** — GitLab, Linear and other trackers behind the same
  connector interface.
- **Security hardening** — httpOnly-cookie sessions, OIDC/SSO, OTel traces (see
  [SECURITY.md](SECURITY.md)).

---

## Principles carried through every phase

- Every phase keeps the **single-binary + `make up`** story intact.
- New capability ships with **tests** and **docs** (README + TESTING).
- Backwards-compatible store migrations; the seed workflow always runs offline.
- No feature is "done" until `go test ./...`, `go vet`, and the UI build are green.
