# Conduit Agent

<p align="center">
  <a href="docs/video/conduit-intro.mp4">
    <img src="docs/video/preview.gif" width="960"
      alt="Conduit Agent sample run: a Jira ticket is triaged and planned by an AI model, a person approves twice, a branch is pushed, CI passes and a pull request opens.">
  </a>
</p>
<p align="center">
  ▶ <a href="docs/video/conduit-intro.mp4"><b>Watch the 35-second intro</b></a>
  &nbsp;·&nbsp; <a href="docs/video/conduit-intro-vertical.mp4">vertical 9:16 cut</a>
</p>

**Tickets in. Reviewed pull requests out.**

Conduit Agent is an open-source, self-hosted **AI software factory**. Give it a
ticket and it produces a pull request, with a person approving at every step that
matters:

1. **A ticket arrives** from Jira, Notion, a GitHub issue, a webhook, or a task on
   the built-in board.
2. **Triage scores it.** A vague ticket goes to a person as "needs clarification"
   instead of to a model.
3. **Agents plan the change**, draft the implementation and review it.
4. **You approve the plan**, then the agents produce the exact list of file changes.
5. **You approve those files.** Nothing is written to your repository before this.
6. **A coding agent writes each file and pushes a branch** through the GitHub API.
7. **CI has to pass.** A failing check goes back to the coding agent as feedback,
   up to twice.
8. **The pull request opens**, the link is posted on the ticket, and the PR lands in
   a GitHub review workflow where an agent reviews it and a person approves the merge.

Every step is a node on a visual graph you can watch live, skip, or re-run from
the stage that failed. You bring your own repository and model (Anthropic, OpenAI,
or your own gateway). Each person who signs in gets their own copy of the
workflows, with their settings and secrets encrypted and kept to their login.

Under the hood it is a general workflow engine: a workflow is a graph of typed
**components** (trigger → steps → status) wired with `success` / `failed` /
`approved` edges, so the same canvas builds any event-driven workflow — webhooks,
schedules, HTTP and Slack calls, conditions and approval gates.

It ships as a **single Go binary**: the Go backend (engine + REST + SSE) serves the
API, and a **Next.js** frontend is statically exported and embedded into the same
binary, so there is no Node runtime in production. MIT licensed.

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
    ├── components/         # executors: triage, agent, coding_plan/apply, github_*, jira, notion, http, if, …
    ├── engine/             # DAG walker; runs nodes, follows edges, gates on approval
    ├── seed/               # built-in workflows (AI Factory, GitHub review, Preview Environments) + per-user copies
    └── api/                # HTTP routes (REST) + Server-Sent Events stream
```

The store is an interface (`internal/store`) with two implementations:
`Mem` (in-memory, the default) and `Pg` (Postgres). The engine and API depend on
the interface, so swapping persistence is a one-line change in `main.go`.

**Docs:** [Technical Design Document](docs/tdd.html) (interactive — architecture,
stack, data model, execution engine, sequence flows; also served live at
**`/docs`** on a running instance) · [GUIDE.md](GUIDE.md) (user walkthrough) ·
[TESTING.md](TESTING.md) (reproducible test guide).

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
>
> **Want the design?** See the **[Technical Design Document](docs/tdd.html)** —
> an interactive HTML doc covering the architecture, technology stack, data model,
> execution engine, end-to-end sequence flows, the component catalog and the AI
> approach. A clickable architecture diagram and a step-through sequence player are
> built in. It's embedded in the binary and served live at **`/docs`** (e.g.
> `http://localhost:8080/docs`) — or open `docs/tdd.html` in a browser.

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

### Deploying behind a managed platform / reverse proxy (Coolify, Dokploy, …)

`docker-compose.yml` does **not** publish a host port — the app only `expose`s
`8080` on the Docker network, so the platform's proxy routes to it and there is no
"port 8080 already allocated" conflict. In the platform:

1. Point the app service's **domain at container port `8080`**.
2. Set the usual env vars (`CONDUIT_JWT_SECRET`, `CONDUIT_SECRET_KEY`,
   `CONDUIT_BASE_URL`, and any OAuth credentials).

Local `make up` still opens on the host via `docker-compose.override.yml` (which
managed platforms ignore, since they deploy with an explicit `-f
docker-compose.yml`). If a previous failed deploy left an orphaned container
holding the port, remove it once: `docker rm -f $(docker ps -aq --filter name=app-)`.

### Configuration

| Variable | Default | Meaning |
|----------|---------|---------|
| `CONDUIT_ADDR` | `:8080` | listen address |
| `CONDUIT_DATABASE_URL` | _(unset)_ | Postgres DSN; in-memory when unset |
| `CONDUIT_SECRET_KEY` | _(unset)_ | passphrase for encrypting stored secrets; ephemeral (secrets reset on restart) when unset |
| `CONDUIT_JWT_SECRET` | _(unset)_ | secret for signing login JWTs; ephemeral (sessions reset on restart) when unset |
| `CONDUIT_ADMIN_EMAIL` | `admin@conduit.local` | bootstrap admin email (created only if there are no users) |
| `CONDUIT_ADMIN_PASSWORD` | `conduit` | bootstrap admin password — change it after first login |
| `CONDUIT_ALLOW_SIGNUP` | `true` | allow self-serve account creation (`POST /api/auth/register`); set `false` for admin-provisioned accounts only |
| `CONDUIT_HTTP_PORT` | `8080` | **local only** — host port published by `docker-compose.override.yml` for `make up` (set if 8080 is busy locally). Managed deploys don't publish a host port; they route to container port 8080. |
| `CONDUIT_BASE_URL` | _(derived)_ | public origin used to build OAuth redirect URIs (e.g. `https://conduit.example`) |
| `CONDUIT_GOOGLE_CLIENT_ID` / `CONDUIT_GOOGLE_CLIENT_SECRET` | — | enable **Sign in with Google**; redirect URI `…/api/auth/oauth/google/callback` |
| `CONDUIT_GITHUB_CLIENT_ID` / `CONDUIT_GITHUB_CLIENT_SECRET` | — | enable **Sign in with GitHub**; redirect URI `…/api/auth/oauth/github/callback` |
| `CONDUIT_RATE_LIMIT` | `20` | per-IP request/second limit on `/api` (0 disables) |
| `CONDUIT_WORKERS` | `4` | run-executor workers per instance |
| `CONDUIT_CLAUDE_BIN` | `claude` | path to the Claude Code CLI used by `coding_engine=claude_code` |
| `CONDUIT_CLAUDE_SERVER_LOGIN` | `false` | let projects use this server's own logged-in Claude Code (`claude_auth=server_login`). Only for a single-user / trusted self-host. |
| `CONDUIT_WORK_DIR` | system temp | where Claude Code checkouts are created (and removed afterwards) |
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

## The AI Factory — ticket → reviewed PR

The **AI Factory** (the `ai-factory` project, workflow `wf_factory`) is a complete
one‑shot‑engineering pipeline: a real ticket becomes a reviewed pull request with
two human gates, a real CI verification step, and automatic recovery. It is seeded
on first run and surfaced two ways — as a **workflow** on the canvas, and as a
**board** of durable tasks (Backlog → Implement → Verify → Done → Blocked).

```
Ticket (Jira/Notion/webhook)
  → Triage · Confidence ──low──▶ Needs clarification (human)
  → Refine & Plan → Draft → Review          (agents)
  → Human Approval  (the plan)
  → Plan Changes → Approve Changes (the exact files) → Apply Changes (pushes a branch)
  → Verify Checks (CI) ──fail──▶ retry ×2 back to the coding agent ──exhausted──▶ Needs attention
  → Open PR → comment the PR on the ticket → Record Delivery
```

### 1. Configure once (Settings → Variables / Secrets)

Everything is set per project and referenced from nodes as `${var:NAME}` /
`${secret:NAME}` — no per‑node repetition. Switch to the **AI Factory** project,
open **Settings**, and set:

**Variables** (plain, visible)

| Variable | Example | Used by |
| --- | --- | --- |
| `owner` | `acme` | coding agent, PR, checks |
| `repo` | `web` | coding agent, PR, checks |
| `jira_base` | `https://acme.atlassian.net` | Jira fetch / comment |
| `jira_email` | `bot@acme.com` | Jira auth |
| `jira_jql` | `project = ENG AND status = "Ready for Dev" ORDER BY created ASC` | Jira fetch |
| `llm_provider` | `anthropic` | all agents |
| `llm_model` | `claude-sonnet-4-6` (or your model) | all agents |
| `auto_start_confidence` | `70` *(optional)* | webhook intake auto‑start |

**Secrets** (encrypted, write‑only)

| Secret | What | Scope needed |
| --- | --- | --- |
| `llm_key` | model API key | — |
| `jira_token` | Atlassian API token | read issues + add comments |
| `github_token` | GitHub token | **`contents:write` + `pull_requests:write`** (the coding agent commits a branch, opens the PR, and reads check‑runs) |
| `notion_token` | *(only if using Notion as the source)* | read the database |

> The branch base is `main` by default. For a GitHub‑only review/merge flow the
> `github` project additionally uses `scan_token` (the security‑scan gate).

### Choose how code is written: Claude Code or the direct API

**Settings → Coding agent** picks the engine for *Plan changes* and *Apply changes*.
Both keep the same approval gates; nothing is pushed before the file list is approved.

| | **Claude Code** (`coding_engine=claude_code`) | **Direct API** (`coding_engine=api`, default) |
| --- | --- | --- |
| How it works | A real Claude Code session in a fresh clone of the repo: it reads and searches the whole codebase, edits files, and can run the commands you allow (e.g. the test suite) before finishing | One model call to plan, then one call per file with that file's current content |
| Sign-in / billing | Your **Claude subscription** (`claude setup-token` → secret `claude_code_oauth_token`) *or* an **Anthropic API key** (secret `anthropic_api_key`, falling back to an Anthropic `llm_key`) | Your `llm_provider` / `llm_model` / `llm_key` (Anthropic, OpenAI or a custom gateway) |
| Pushing | Conduit commits and pushes after checking that only approved files changed; an unapproved file fails the step and nothing is pushed | Conduit builds the commit through the GitHub API |
| Needs on the server | `git` and the Claude Code CLI (both in the default Docker image) | nothing extra (works with the `slim` image) |

Claude Code settings (all optional except the sign-in):

| Variable | Default | What it does |
| --- | --- | --- |
| `claude_auth` | automatic | `subscription`, `api_key`, or `server_login` (the server's own `claude` login — only when the operator sets `CONDUIT_CLAUDE_SERVER_LOGIN=true`, e.g. a single-user self-host). Automatic picks a subscription token, then an API key. |
| `claude_model` | CLI default | `sonnet`, `opus`, `haiku`, or a full model ID |
| `claude_allowed_commands` | none | comma-separated command prefixes it may run, e.g. `npm ci, npm test`. Empty = no shell at all; CI still tests the branch. `git push` is never allowed. |
| `claude_timeout_minutes` | 10 plan / 20 apply | stop a session that runs longer |
| `claude_max_budget_usd` | none | spend cap per session (API-key billing only) |

**Save & test connection** in that tab runs one tiny prompt with the project's sign-in
and reports whether it worked.

How a session is isolated: a throwaway clone and an empty home directory; an
environment built from an allowlist (no server secrets — not the database URL, the
encryption keys or the GitHub token, which only Conduit's own `git` uses);
deny-by-default permissions with reads and edits limited to the checkout; no web
tools, no MCP servers, and the repository's own `.claude` settings ignored (its
`CLAUDE.md` is still read). The plan session is read-only. Live progress
("Claude Code · Edit app/layout.tsx") streams to the run's console, and **Cancel**
stops the session and anything it started.

### 2. Run it — two ways

**A. From the board (manual).** Open the **Board** tab → **New task** (or it
appears from intake). A card lands in **Backlog**. Click **Start** — the factory
runs bound to that task, and the card advances Backlog → Implement → Verify → Done
live. The task’s ticket is used as‑is (no Jira needed for a manual task).

**B. From a webhook (automatic intake).** Mint an API token
(`POST /api/tokens`), then point your issue tracker at the intake endpoint. The
payload is normalized for `source=github|jira`, or send the generic shape
(Notion automations, anything):

```bash
# GitHub issue webhook → a Backlog task, scored for readiness
curl -X POST "https://YOUR_HOST/api/intake/ai-factory?source=github&token=$CONDUIT_API_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"issue":{"number":482,"title":"Add rate limiting","body":"Throttle /api/* to 100 req/min. Acceptance: a test covers the limiter.","html_url":"https://github.com/acme/web/issues/482"},"repository":{"full_name":"acme/web"}}'
```

Intake scores **confidence** from the ticket (clear outcome, bounded scope, a
checkable success condition). If you set `auto_start_confidence`, a task that
clears it **auto‑starts**; otherwise it waits in Backlog for a human. Ambiguous
tickets are never turned into a confident‑looking wrong change.

### 3. What happens during a run

- **Triage** gates intake again inside the run; a low score routes to **Needs
  clarification** instead of implementing.
- Three agents **refine → draft → review** the ticket; the first **Human
  Approval** shows the plan + review (and the confidence) right at the gate.
- **Coding plan → Approve Changes → Apply:** the coding agent first proposes the
  exact files (create/modify), you approve that list, then it pulls the current
  contents, edits them, and **pushes a branch via the GitHub Git Data API** (no
  local checkout).
- **Verify Checks (CI):** waits for the repo’s own check‑runs on the pushed
  commit. On failure it loops back to the coding agent **up to twice**, feeding the
  failing checks back as context; only after the budget is spent does it go to
  **Needs attention**. A repo with no CI configured passes straight through.
- **Open PR** (idempotent) → comments the PR link back on the ticket → **Record
  Delivery**; the task moves to **Done** with the PR and branch captured.

### 4. Try it end‑to‑end (smoke test, no credentials)

You can exercise the whole shape offline — triage uses an offline heuristic when
no `llm_key` is set, and a repo with no CI passes Verify:

1. **Board → New task**, title *“Add a CONTRIBUTING note; acceptance: file exists.”*
   (a clear, checkable ask scores as ready).
2. Click **Start**. Watch the **Editor** run the graph live (or click **View run**
   on the card). It pauses at **Human Approval** — the banner shows the plan.
3. **Approve.** It plans the file changes and pauses at **Approve Changes** with
   the exact file list. **Approve** again.
4. With real `owner`/`repo`/`github_token`, it pushes a branch, verifies CI, opens
   a PR, and the card lands in **Done** with a **PR** link. Without them, the
   coding step reports what it needs — the earlier stages still demonstrate the
   full plan → approve → apply flow.
5. Open the **Metrics** tab: delivered count, average lead time, per‑stage dwell,
   the stage‑flow funnel, auto‑recovery rate and intervention rate update live.

### 5. Use Notion instead of Jira

Swap the **Fetch Jira Ticket** node for the **Notion** component from the palette,
set `notion_db` (and a `notion_token` secret), or just drive it from the board /
webhook intake — both write the same `ticket_*` memory, so the rest of the pipeline
is unchanged.

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
| `GET`  | `/api/events` | SSE stream (optional `?run=<id>`; omit for all events incl. `task.*`) |
| `GET`  | `/api/tasks` | list board tasks (optional `?stage=`) |
| `POST` | `/api/tasks` | create a task in Backlog |
| `GET`  | `/api/tasks/{id}` | task detail + run history |
| `POST` | `/api/tasks/{id}/start` | start the factory for a task |
| `POST` | `/api/tasks/{id}/stage` | move a task to a stage (`{stage}`) |
| `POST` | `/api/tasks/{id}/notes` | add a comment (`{text}`) |
| `DELETE` | `/api/tasks/{id}` | delete a task |
| `POST` | `/api/intake/{project}` | webhook intake (token‑authed; `?source=github\|jira`) |
| `GET`  | `/api/metrics` | factory‑performance summary for the project |

## Roadmap

What has shipped, phase by phase, and what comes next is in **[ROADMAP.md](ROADMAP.md)**
(execution & triggers → scheduling & parallelism → integrations & secrets →
multi-tenancy & auth → realtime/scale → the AI Factory → release hardening).

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

**Phase 6 (shipped):** the AI Factory —

- **Agents** (`agent`, `triage`) against Anthropic / OpenAI / a custom gateway, with
  prompts templated from run state, project settings and secrets.
- A real **ticket → PR pipeline**: Jira / Notion / GitHub-issue / board intake →
  triage → plan → two human approvals → a coding agent that pushes a branch → CI
  verification with retry → pull request → GitHub review workflow handoff.
- **Run control**: skip any step, re-run from the failed stage, cancel, live status.
- **Per-user workspaces**: each signed-in user gets their own copy of the workflows,
  with settings and secrets encrypted at rest. See
  [The AI Factory](#the-ai-factory--ticket--reviewed-pr) below.

**Phase 7 (shipped — `v1.0.0`):** release hardening —

- **SSRF guard** on outbound executors (private/loopback blocked at dial time;
  `CONDUIT_ALLOW_PRIVATE_EGRESS` to opt in), a 4 MiB request-body cap, and
  security headers. See **[SECURITY.md](SECURITY.md)** for the security model and
  production checklist.

Next: lower model spend per run, bring-your-own coding agent, more model backends,
resumable execution and security hardening — see
[What's next](ROADMAP.md#whats-next).

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
