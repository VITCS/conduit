# Conduit

A small, runnable control plane for **event-driven automation workflows** — a
visual DAG of components you execute, watch live, and gate on human approval.

Conduit is an original take on the pattern popularized by control-plane tools
like SuperPlane: a workflow is a graph of typed **components** (trigger → steps →
status), wired with `success` / `failed` / `approved` edges. A **run** walks that
graph, executes each component, streams status to the UI, and pauses at approval
gates until someone signs off.

This repo is a **full-stack app** that ships as a **single Go binary**: the Go
backend (engine + REST + SSE) serves an API, and a **Next.js** frontend is
statically exported and embedded into that same binary — no Node runtime in
production.

```
conduit/
├── main.go                 # entrypoint; embeds web/out, wires store+engine+api
├── web/                    # Next.js app (TypeScript, React Flow canvas)
│   ├── app/                #   page.tsx (orchestrator), layout, globals.css
│   ├── components/         #   ConduitNode, ConduitEdge, Inspector, Topbar, RunRail, Palette
│   ├── lib/                #   api client, types, catalog, icons, editor context
│   └── out/                #   `next build` output — embedded by Go (git-ignored in real use)
└── internal/
    ├── model/              # Workflow, Node, Edge, Run, StepRun, Event
    ├── store/              # Store interface + Mem (in-memory) and Pg (Postgres) + pub/sub broker
    ├── components/         # executors: http, ssh, timegate, if, wait, memory, status…
    ├── engine/             # DAG walker; runs nodes, follows edges, gates on approval
    ├── seed/               # the default "Preview Environments" workflow
    └── api/                # HTTP routes (REST) + Server-Sent Events stream
```

The store is an interface (`internal/store`) with two implementations:
`Mem` (in-memory, the default) and `Pg` (Postgres). The engine and API depend on
the interface, so swapping persistence is a one-line change in `main.go`.

## Quickstart — one command (recommended)

The only prerequisite is **Docker**. This builds the backend **and** the UI into
a single image and runs it together with Postgres — nothing else to install, no
Go, no Node, no separate frontend to start:

```bash
make up          # == docker compose up --build
```

Then open <http://localhost:8080>. Press **Run** and watch the pipeline execute;
it pauses at **Require Approval** — click that node and **Approve** (or **Reject**
to see the failure branch). Data persists in a Postgres volume across restarts.

```bash
make logs        # tail logs
make down        # stop (keep data)
make down-v      # stop and wipe the database volume (fresh start)
```

That's the whole testing loop: **build once, run everything at once.**

> **New here?** Start with **[GUIDE.md](GUIDE.md)** — a step-by-step walkthrough
> from local/Docker setup through using the app as a real user, with a concrete
> real-world scenario (PR preview environments) that shows what Conduit is for.
> The full reproducible test guide is in [TESTING.md](TESTING.md).

## Build & run from source (without Docker)

Requires Go 1.24+ and Node 20.9+ (Node is only needed to build the UI). The Go
binary embeds the UI, so there is no Node runtime at run time.

```bash
make build       # make ui (next build) + go build -> ./conduitd (single binary)
./conduitd       # open http://localhost:8080
```

For development with UI hot-reload, see below. Set `CONDUIT_ADDR` to change the
listen address, and `CONDUIT_DATABASE_URL` to use Postgres (otherwise in-memory).

### UI development (hot reload)

```bash
make run                       # terminal 1: Go API on :8080
make run-dev                   # terminal 2: next dev on :3000 (NEXT_PUBLIC_API_BASE -> :8080)
```

Edit under `web/`; when done, `make ui` re-exports and `make build` re-embeds.

### Persistence

By default state is in-memory and resets on restart. Point it at Postgres to
persist workflows and runs:

```bash
export CONDUIT_DATABASE_URL="postgres://user:pass@localhost:5432/conduit?sslmode=disable"
./conduitd   # logs "persistence: postgres"; schema is created automatically
```

The schema (two JSONB-backed tables, `workflows` and `runs`) is applied on
startup — no migration tool needed. `MutateRun` loads the row `FOR UPDATE` inside
a transaction, so concurrent step writes serialize on the run.

## Deploy

Run the whole stack (app + Postgres, with a persistent volume) in one command:

```bash
docker compose up --build      # http://localhost:8080
```

Or build and run just the container (in-memory):

```bash
docker build -t conduit .
docker run --rm -p 8080:8080 conduit
```

The image is a multi-stage build — Node builds the UI, Go embeds it, and the
final stage is a distroless, non-root static image containing only the binary.

### Configuration

| Variable | Default | Meaning |
|----------|---------|---------|
| `CONDUIT_ADDR` | `:8080` | listen address |
| `CONDUIT_DATABASE_URL` | _(unset)_ | Postgres DSN; in-memory when unset |
| `CONDUIT_SECRET_KEY` | _(unset)_ | passphrase for encrypting stored secrets; ephemeral (secrets reset on restart) when unset |
| `CONDUIT_JWT_SECRET` | _(unset)_ | secret for signing login JWTs; ephemeral (sessions reset on restart) when unset |
| `CONDUIT_ADMIN_EMAIL` | `admin@conduit.local` | bootstrap admin email (created only if there are no users) |
| `CONDUIT_ADMIN_PASSWORD` | `conduit` | bootstrap admin password — change it after first login |
| `CONDUIT_RATE_LIMIT` | `20` | per-IP request/second limit on `/api` (0 disables) |
| `CONDUIT_WORKERS` | `4` | run-executor workers per instance |
| `CONDUIT_ALLOW_PRIVATE_EGRESS` | `false` | allow outbound executors to reach private/loopback addresses (SSRF guard off) |

### Production notes

- **Single binary**: the UI is embedded, so there is no Node runtime or static
  file dependency to deploy alongside it.
- **Graceful shutdown**: `SIGINT`/`SIGTERM` drains in-flight requests (15s) before
  exit, so container stops and rolling deploys don't cut active runs mid-step.
- **Timeouts**: the HTTP server sets `ReadHeaderTimeout` and `IdleTimeout`
  (slowloris guard) while leaving the SSE stream long-lived.
- **Caching**: content-hashed `/_next/static` assets are served `immutable`.
- Before exposing publicly, add **auth** (there is none yet) and consider moving
  the in-process SSE broker to Postgres `LISTEN/NOTIFY` for multi-instance.

## Testing

See **[TESTING.md](TESTING.md)** for a complete, reproducible guide: automated Go
tests, the Postgres contract test, the UI build and browser smoke test, a manual
UI walkthrough, API testing with `curl`, live SSE testing, validation testing,
and durability checks.

## Editor

Toggle **Edit** to turn the canvas into a builder:

- **Add** components from the right-hand palette.
- **Connect** nodes by dragging from a node's right port to another node.
- **Label an edge** (the gate): select an edge and pick `any` / `success` /
  `failed` / `approved`, or delete it.
- **Edit a node** in the inspector's **Config** tab: title, component type, and
  add/remove/rename configuration fields.
- **Delete** a selected node with the trash button or the Delete key.
- **Save** writes the whole workflow to `PUT /api/workflows/{id}` (durable with
  Postgres). Unsaved edits mark the Save button and warn on navigation.

Saves are **validated server-side** (`internal/validate`): the API rejects a
workflow with `422` and a list of problems if it has a **cycle**, a **dangling
edge** (endpoint pointing at a missing node), a **self-loop**, duplicate node IDs
or edges, an unknown component type, an invalid kind or edge label, or a missing
name. The reasons are surfaced in the UI's save toast, e.g.
`workflow is invalid: workflow has a cycle: a1 → u1 → t1 → …`.

## Tests

```bash
go test ./...            # unit + integration; Postgres tests skip without a DB
go test -race ./...      # same, under the race detector
```

Coverage: the DAG engine (success, approval, reject, failure branch, `skipped`
propagation, edge matching), component executors, the store contract, and the
HTTP API (run lifecycle, approval, error paths). The store contract test also
runs against real Postgres when a DSN is provided — it is skipped otherwise, so
the default `go test ./...` needs no database:

```bash
# spin up any Postgres, then:
export CONDUIT_TEST_POSTGRES="postgres://user:pass@localhost:5432/conduit?sslmode=disable"
go test ./internal/store/ -run TestPostgresStoreContract -v
```

## How a run works

1. `POST /api/runs {workflow_id}` (or a webhook/schedule) **enqueues** a run
   (`queued`). A worker from the pool **claims** it with a lease and executes it;
   across instances, exactly one worker runs a given run.
2. The engine starts at the trigger node and walks the DAG, running independent
   branches concurrently. For each node it marks `running`, calls the component
   executor, records the output, and follows outgoing edges whose label matches
   the outcome (`success` / `failed` / `approved`).
3. Every transition is published to the broker and streamed to the browser over
   `GET /api/events?run=<id>` (SSE), so the canvas, run rail, and console update
   live.
4. An **approval** node sets the run to `waiting` and blocks until
   `POST /api/runs/{id}/approve {node_id, decision}` delivers a decision.

## API

All `/api` routes require authentication (a `Bearer` JWT from login, or an API
token) except `GET /api/health`, `POST /api/auth/login`, and the static UI.
Write methods (`POST`/`PUT`/`DELETE`) require a non-viewer role.

| Method | Path | Purpose |
|---|---|---|
| `GET`  | `/api/health` | liveness (public) |
| `POST` | `/api/auth/login` | email+password → `{token, user}` (public) |
| `GET`  | `/api/auth/me` | current user |
| `GET`  | `/api/tokens` | list API tokens |
| `POST` | `/api/tokens` | create an API token (`{name, role}`) — plaintext shown once |
| `DELETE` | `/api/tokens/{id}` | revoke an API token |
| `GET`  | `/api/projects` | list projects |
| `POST` | `/api/projects` | create a project (`{name}`) — creator becomes owner |
| `GET`  | `/api/projects/{id}/members` | list members |
| `POST` | `/api/projects/{id}/members` | add a member (`{email, role}`) — project owner/admin |
| `DELETE` | `/api/projects/{id}/members/{userID}` | remove a member |
| `GET`  | `/api/users` | list users (global admin) |
| `POST` | `/api/users` | create a user (`{email, name, password, role}`) — global admin |

Workflow, run and **secret** endpoints are scoped to a **project**, selected with
the `X-Conduit-Project` header (default: `default`). A user must be a **member**
of the selected project (global owner/admin override); non-members get 403, and
cross-project reads are 404.
| `GET`  | `/api/components` | registered component types |
| `GET`  | `/api/workflows` | list workflows |
| `GET`  | `/api/workflows/{id}` | one workflow |
| `PUT`  | `/api/workflows/{id}` | create/replace a workflow (`422` with `details[]` if invalid) |
| `POST` | `/api/runs` | start a run (`{workflow_id}`) |
| `GET`  | `/api/runs` | list runs (newest first) |
| `GET`  | `/api/runs/{id}` | one run with steps + memory |
| `POST` | `/api/runs/{id}/approve` | resolve an approval gate (`{node_id, decision}`) |
| `POST` | `/api/runs/{id}/cancel` | cancel a running/waiting run (`409` if not active) |
| `POST` | `/api/hooks/{workflowId}` | start a run from a webhook; JSON body becomes the trigger input |
| `GET`  | `/api/schedules` | list active cron schedules |
| `GET`  | `/api/audit` | recent audit entries (global admin) |
| `GET`  | `/metrics` | Prometheus metrics (public, not under `/api`) |
| `GET`  | `/api/secrets` | list secret names (never values) |
| `PUT`  | `/api/secrets/{name}` | create/update a secret (`{value}`) |
| `DELETE` | `/api/secrets/{name}` | delete a secret |
| `GET`  | `/api/events` | SSE stream (optional `?run=<id>`) |

## Roadmap

The plan to grow Conduit toward full SuperPlane parity is in **[ROADMAP.md](ROADMAP.md)**
(phased: scheduling & parallelism → integrations & secrets → multi-tenancy & auth
→ realtime/scale → agents → release hardening).

**Phase 1 (shipped):** execution robustness & inbound triggers —

- **Retries with backoff** — per-node `retries` + `retry_backoff_ms`.
- **Run cancellation** — `POST /api/runs/{id}/cancel`; context-aware engine,
  `Canceled` status, Cancel button in the UI.
- **Webhook trigger** — `POST /api/hooks/{workflowId}` starts a run and binds the
  JSON body as the trigger's `input`, available to downstream steps.
- **Real HTTP executor** — opt-in (`live: true`) nodes make a real request
  (method/headers/body/timeout); the simulated default keeps the demo offline.

**Phase 2 (shipped):** scheduling, parallelism & run history —

- **Parallel execution** — independent DAG branches run concurrently; retries,
  cancellation and approval gating are preserved.
- **Cron scheduling** — `schedule` trigger nodes (`cron` field) fire runs via a
  background scheduler; `GET /api/schedules` lists them; re-syncs on save.
- **Run history** — the rail lists past runs; pick one to load its state.
- **New components** — `filter` (stops a branch without failing), `merge`,
  `graphql`, and `schedule`/`start` triggers.

**Phase 3 (shipped):** integrations & secrets —

- **Encrypted secret store** (AES-256-GCM; `CONDUIT_SECRET_KEY`), write-only via
  the API (`/api/secrets`) and a Secrets panel in the UI.
- **`${secret:NAME}` resolution** in node config, so credentials stay out of the
  workflow graph.
- **Real connectors**: a `slack` component and a secret-aware live HTTP node.
  Executors now run off the store lock, so network I/O can't block the store.

**Phase 4a (shipped):** authentication & RBAC —

- **Password login** (bcrypt) issuing stateless **JWTs**; a bootstrap admin is
  created on first run. Sign-in screen + user chip in the UI.
- **API tokens** (hashed at rest) for CI and to authenticate webhooks.
- **RBAC**: owner/admin/member/viewer; auth middleware guards every `/api` route;
  viewers are read-only.

**Phase 4b (shipped):** multi-tenancy (projects) —

- **Projects** scope workflows and runs (`project_id`); every list/get/create is
  filtered by the selected project (`X-Conduit-Project` header), cross-project
  access is 404.
- Project CRUD + a **project switcher** in the UI; new projects get a starter
  workflow; a `default` project is seeded.

**Phase 4c (shipped):** per-project membership —

- **Membership-gated access**: a user reaches a project only if they are a member
  (global owner/admin override); the per-project role gates writes. Non-members
  get 403 — true isolation by user.
- **Members & users** API + UI (add/remove members, create users).
- **Project-scoped secrets**: secrets live under a project and resolve per-run.

**Phase 5 (shipped):** realtime, scale & ops —

- **Postgres `LISTEN/NOTIFY`** event bus so live run events (SSE) fan out across
  multiple instances behind a load balancer (auto-enabled with a DB).
- **Observability**: Prometheus `/metrics`, structured JSON logs with request
  IDs, and an **audit log** of mutations (`/api/audit`, admin).
- **Rate limiting** (per-IP token bucket, `CONDUIT_RATE_LIMIT`).

**Phase 5b (shipped):** worker model —

- Runs are **enqueued** (`POST /api/runs`, webhooks, schedules) and executed by a
  **worker pool** that claims each run with a **lease** (Postgres `SKIP LOCKED`),
  so exactly one worker across all instances runs it; a dead worker's lease
  expires and the run is reclaimed. Size the pool with `CONDUIT_WORKERS`.

**Phase 6 (shipped):** agents / factory —

- An **`agent`** component that prompts an LLM — **simulated by default** (offline),
  or **live** against Anthropic / OpenAI / a custom gateway using a project secret
  for the API key. Prompts template run state via `${input.key}` / `${memory.key}`.
- A seeded **AI Factory** workflow (in the `ai-factory` project) modeling
  *issue → refine → implement → verify → review → approve → open PR*.

**Phase 7 (shipped — `v1.0.0`):** release hardening —

- **SSRF guard** on outbound executors (private/loopback blocked at dial time;
  `CONDUIT_ALLOW_PRIVATE_EGRESS` to opt in), a 4 MiB request-body cap, and
  security headers. See **[SECURITY.md](SECURITY.md)** for the security model and
  production checklist.

Post-1.0 (tracked in SECURITY.md): resumable execution (reclaim resumes rather
than re-runs; also enables cross-instance approval), httpOnly-cookie sessions,
OTel traces, agent tool-use + coding-agent sandbox, and OIDC/SSO.

Previously shipped: **Postgres persistence**, a **`go test` suite**, the
**Next.js/React Flow UI** embedded in the single binary, **editor saves**, an
**edge label/gate picker**, **inline node config editing**, and **save-time
validation** (cycles, dangling edges, …).

### Dependencies

`npm audit` reports **0 vulnerabilities** (Next.js 16.4.3). The UI is a static
export served by Go as plain files, so Next's server-runtime attack surface
isn't shipped regardless. The only warning at build time is that React 18 is
deprecated under Next 16 — functional today, and a React 19 upgrade is listed
above.
