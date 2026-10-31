# Conduit Roadmap — from mini-clone to full product

This is the development plan to take Conduit from the current working slice to a
full-featured, production-grade control plane in the spirit of SuperPlane.

## Where we are (shipped)

- Workflow engine: DAG executor, `success`/`failed`/`approved` edge routing,
  human approval gates, `skipped` propagation.
- Store interface with in-memory + Postgres, JSONB, row-locked mutations.
- REST + SSE API, save-time validation (cycles, dangling edges, …).
- Next.js 16 / React Flow UI (canvas editor, run rail, inspector, console,
  memory), embedded into a single Go binary.
- One-command Docker (`make up`), CI, test suite, docs.

## Target feature set (SuperPlane parity)

Grouped by theme, with the components/concepts each phase adds.

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
| Agents | LLM/coding-agent components (the "factory" angle) — optional track |
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

### Phase 4 — Multi-tenancy & auth

- **Organizations → projects → workflows** data model and scoping on every query.
- **AuthN**: OIDC login + API tokens; session middleware.
- **RBAC**: roles (owner/admin/member/viewer) and permission checks in the API.
- **Members & invitations**, per-project settings.

### Phase 5 — Realtime, scale & ops

- **Postgres `LISTEN/NOTIFY`** event bus so SSE works across multiple instances.
- **Worker model**: separate run-executor workers from the API; at-least-once
  execution with claim/lease on runs.
- **Observability**: Prometheus metrics, OTel traces, structured JSON logs,
  **audit log**; rate limiting and request size limits.

### Phase 6 — Agents / factory (optional track)

- LLM + coding-agent components, memory-backed context, and the "turn an issue
  into a reviewed PR" factory flow, behind the connector/secret framework.

### Phase 7 — Release hardening

- Security review (authz coverage, SSRF guards on the HTTP executor, input
  limits), load testing, backup/restore docs, versioned API, `v1.0.0`.

---

## Principles carried through every phase

- Every phase keeps the **single-binary + `make up`** story intact.
- New capability ships with **tests** and **docs** (README + TESTING).
- Backwards-compatible store migrations; the seed workflow always runs offline.
- No feature is "done" until `go test ./...`, `go vet`, and the UI build are green.
