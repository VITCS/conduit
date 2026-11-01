# Testing & Usage Guide

This document is a complete, reproducible guide to running Conduit and verifying
every part of it: the Go backend, the workflow engine, the REST + SSE API, the
Next.js UI, Postgres persistence, and save-time validation.

- [1. Prerequisites](#1-prerequisites)
- [2. Running the app](#2-running-the-app)
- [3. Automated tests (Go)](#3-automated-tests-go)
- [4. Testing against Postgres](#4-testing-against-postgres)
- [5. Frontend build & smoke test](#5-frontend-build--smoke-test)
- [6. Manual UI walkthrough](#6-manual-ui-walkthrough)
- [7. API testing with curl](#7-api-testing-with-curl)
- [8. Live events (SSE) testing](#8-live-events-sse-testing)
- [9. Validation testing](#9-validation-testing)
- [10. Persistence & durability](#10-persistence--durability)
- [11. Docker](#11-docker)
- [12. Troubleshooting](#12-troubleshooting)

---

## 1. Prerequisites

| Tool | Version | Needed for |
|------|---------|-----------|
| Go | 1.24+ | backend, tests |
| Node.js + npm | 20.9+ | building the UI (not needed to run the binary) |
| Docker | any recent | container / compose path (optional) |
| Postgres | 16 | persistence tests (optional; Docker works too) |

Everything below assumes the repository root (`conduit/`) as the working
directory unless stated otherwise.

---

## 2. Running the app

### Fastest path — one command (Docker only)

If all you want is to run and test the product, you do **not** need Go or Node.
One command builds the backend + UI into a single image and runs it with
Postgres:

```bash
make up          # == docker compose up --build
```

Open <http://localhost:8080> — the canvas loads with the seeded "Preview
Environments" workflow, backed by Postgres (data persists across restarts).

```bash
make logs        # tail logs
make down        # stop (keep data)
make down-v      # stop and wipe the database volume
```

Everything from §6 (UI walkthrough) through §10 (persistence) works against this
single stack. The sections below on building from source are only needed if you
want to develop without Docker.

### From source (requires Go 1.24+ and Node 20.9+)

The Go binary embeds the UI from `web/out`, which is **not** committed (only a
placeholder is). Build the UI once before running from source:

```bash
make ui        # cd web && npm install && npm run build  -> web/out
make run       # go run .   (serves embedded UI + API on :8080)
```

Build a standalone single binary:

```bash
make build     # runs `make ui`, then `go build` -> ./conduitd
./conduitd
```

Configuration (environment variables):

| Variable | Default | Meaning |
|----------|---------|---------|
| `CONDUIT_ADDR` | `:8080` | listen address |
| `CONDUIT_DATABASE_URL` | _(unset)_ | Postgres DSN; in-memory when unset |

---

## 3. Automated tests (Go)

Run the whole suite:

```bash
go test ./...
```

With the race detector (recommended):

```bash
go test -race ./...
```

Verbose, per-package:

```bash
go test -v ./internal/engine/
go test -v ./internal/validate/
go test -v ./internal/api/
go test -v ./internal/store/
go test -v ./internal/components/
```

**What is covered**

| Package | Tests |
|---------|-------|
| `internal/engine` | run reaches approval gate; approve→success; reject→failed; failure branch takes the `failed` edge; unreached nodes become `skipped`; `edgeMatches` truth table; unknown-gate resolve returns false |
| `internal/validate` | seed is valid; cycle rejected; dangling edge rejected; diamond DAG is **not** a false positive; duplicate id / self-loop / unknown type; invalid label / missing name |
| `internal/components` | HTTP success & failure; provision-shape output; memory write; status/noop; if-branch |
| `internal/store` | the `Store` contract (workflows, runs, mutate, snapshot isolation, ordering) against `Mem`, and against Postgres when a DSN is set |
| `internal/api` | health, list, run lifecycle over HTTP, approval, `PUT` round-trip, **invalid `PUT` → 422**, 404/409 paths |

Expected result: every package prints `ok` (packages without tests print
`[no test files]`).

Formatting and static checks (what CI enforces):

```bash
gofmt -l .      # prints nothing when clean
go vet ./...
```

---

## 4. Testing against Postgres

The Postgres store contract test is **skipped** unless `CONDUIT_TEST_POSTGRES`
is set, so `go test ./...` needs no database by default.

Spin up a throwaway Postgres with Docker and run it:

```bash
docker run -d --rm --name conduit-pg \
  -e POSTGRES_USER=conduit -e POSTGRES_PASSWORD=conduit -e POSTGRES_DB=conduit \
  -p 5433:5432 postgres:16-alpine

export CONDUIT_TEST_POSTGRES="postgres://conduit:conduit@localhost:5433/conduit?sslmode=disable"
go test ./internal/store/ -run TestPostgresStoreContract -v

docker rm -f conduit-pg
```

Expected: `--- PASS: TestPostgresStoreContract`.

---

## 5. Frontend build & smoke test

Type-check and export the UI:

```bash
cd web
npm install
npm run build          # TypeScript is checked here; output in web/out
cd ..
```

Expected: `✓ Compiled successfully`, a `/` route generated, and `web/out/` with
`index.html` and a `_next/` directory.

Optional end-to-end browser check (Playwright is preinstalled in CI images and
in this project's dev container):

```bash
# with the server running on :8080
node - <<'EOF'
const { chromium } = require('playwright');
(async () => {
  const b = await chromium.launch({ args: ['--no-sandbox'] });
  const p = await b.newPage();
  const errors = [];
  p.on('pageerror', e => errors.push(e.message));
  await p.goto('http://localhost:8080/', { waitUntil: 'networkidle' });
  await p.waitForSelector('.cnode');
  console.log('nodes:', await p.locator('.cnode').count(), 'errors:', errors);
  await b.close();
})();
EOF
```

Expected: 9 nodes, no errors.

---

## 5c. End-to-end tests (Playwright)

Browser smoke tests drive the **real UI against the real binary**, covering the
path unit tests can't: landing → auth → run → live approval.

```bash
make build                 # produce ./conduitd with the UI embedded
cd web
npm install                # first time (installs @playwright/test)
npm run test:e2e           # launches ./conduitd on :8080 and runs the specs
```

The config (`web/playwright.config.ts`) starts the binary itself; to run against an
already-running server instead, set `E2E_BASE_URL`:

```bash
E2E_BASE_URL=http://localhost:8080 npm run test:e2e
```

`web/e2e/smoke.spec.ts` asserts:
1. the landing page renders real capabilities and the sign-in / create-account CTAs;
2. **login → Run → approval banner appears → Approve → run finishes** (this guards
   the SSE/approval regression — if events don't reach the browser, it fails);
3. **create account** signs a new user into their own workspace.

## 6. Manual UI walkthrough

Start the server (`make run`) and open <http://localhost:8080>.

**Landing & account**
- The intro page describes what Conduit does and offers **Sign in** and **Create
  account** (email only — there is no third-party SSO yet).
- **Create account** registers a new user and drops them into their own workspace
  (self-serve signup; disable it with `CONDUIT_ALLOW_SIGNUP=false`).
- **Sign in** with the bootstrap admin (`admin@conduit.local` / `conduit`).

**Run a workflow**
1. Click **Run** (top right). Node badges flip `Pending → Running → Success` and
   the active edge animates.
2. The run pauses at **Require Approval** (badge `Waiting`); a **persistent banner**
   appears across the top with **Approve / Reject**, plus a desktop notification.
3. Click **Approve** in the banner (or open the node and approve in the inspector).
   The run completes; `Publish Preview URL` and `Record Environment` finish.
4. Open the **Console** tab to see the structured log, and **Memory** to see
   `preview-envs/154` written by the run.
5. Run again and click **Reject** instead — the run ends `failed` and the
   downstream node is `skipped`.

**Edit the workflow**
1. Click **Edit**. A **Components** palette appears on the right; nodes become
   draggable.
2. **Add**: click a palette item (e.g. HTTP). It drops on the canvas, selected.
3. **Connect**: drag from a node's right-hand port to another node.
4. **Label an edge**: click an edge, then pick `any` / `success` / `failed` /
   `approved`, or the `✕` to delete it.
5. **Edit a node**: select it, open the inspector's **Config** tab, change the
   title / type / fields.
6. **Delete**: select a node and press the trash icon or the Delete key.
7. **Save**: click **Save** (shows `Save *` when there are unsaved edits).
   A toast confirms "Workflow saved". Reload the page — edits persist (with
   Postgres they survive a restart; see §10).

**Theme**: the sun/moon button toggles light/dark; the page also follows the OS
theme by default.

---

## 7. API testing with curl

All endpoints are same-origin under `/api`. With the server on `:8080`:

**Health**
```bash
curl -s localhost:8080/api/health
# {"status":"ok","time":"..."}
```

**Component catalog**
```bash
curl -s localhost:8080/api/components
# {"types":["github","webhook","timegate","http","ssh","if","wait","memory","status","noop","approval"]}
```

**List / get workflows**
```bash
curl -s localhost:8080/api/workflows | python3 -m json.tool
curl -s localhost:8080/api/workflows/wf_preview_envs | python3 -m json.tool
```

**Start a run**
```bash
RID=$(curl -s -X POST localhost:8080/api/runs \
  -H 'Content-Type: application/json' \
  -d '{"workflow_id":"wf_preview_envs"}' | python3 -c "import sys,json;print(json.load(sys.stdin)['id'])")
echo "run id: $RID"
```

**Inspect the run** (poll until `waiting`)
```bash
curl -s localhost:8080/api/runs/$RID | python3 -c "import sys,json;r=json.load(sys.stdin);print(r['status'])"
```

**Approve the gate** (node `a1` in the seed workflow)
```bash
curl -s -X POST localhost:8080/api/runs/$RID/approve \
  -H 'Content-Type: application/json' \
  -d '{"node_id":"a1","decision":"approved"}'
# {"ok":true,"decision":"approved"}
```

Then the run finishes `success`:
```bash
curl -s localhost:8080/api/runs/$RID | python3 -c "import sys,json;r=json.load(sys.stdin);print(r['status'], list(r['memory']))"
# success ['preview-envs/154']
```

**Error paths**
```bash
curl -s -o /dev/null -w "%{http_code}\n" localhost:8080/api/runs/nope            # 404
curl -s -o /dev/null -w "%{http_code}\n" -X POST localhost:8080/api/runs/x/approve \
  -H 'Content-Type: application/json' -d '{"node_id":"a1","decision":"approved"}' # 409 (nothing waiting)
```

Full endpoint reference is in [README.md](README.md#api).

---

## 8. Live events (SSE) testing

The event stream powers the live UI. Watch it from the terminal:

```bash
# terminal 1: start a run and stream its events
RID=$(curl -s -X POST localhost:8080/api/runs -H 'Content-Type: application/json' \
  -d '{"workflow_id":"wf_preview_envs"}' | python3 -c "import sys,json;print(json.load(sys.stdin)['id'])")
curl -sN "localhost:8080/api/events?run=$RID"
```

You will see `event: run.started`, a series of `event: step.updated` and
`event: log` frames, then `event: run.waiting`. Approve from another terminal
(see §7) and the stream emits the remaining steps and `event: run.finished`.
Press Ctrl-C to stop. Omit `?run=` to stream events for all runs.

---

## 9. Validation testing

`PUT /api/workflows/{id}` validates the graph and returns **422** with a
`details[]` list if it is malformed. Grab the current workflow first:

```bash
curl -s localhost:8080/api/workflows/wf_preview_envs -o wf.json
```

**Cycle → 422**
```bash
python3 -c "import json;w=json.load(open('wf.json'));w['edges'].append({'from':'u1','to':'t1'});json.dump(w,open('cycle.json','w'))"
curl -s -X PUT localhost:8080/api/workflows/wf_preview_envs \
  -H 'Content-Type: application/json' --data @cycle.json -w "\nHTTP %{http_code}\n"
# {"details":["workflow has a cycle: a1 → u1 → t1 → ..."],"error":"workflow is invalid"}
# HTTP 422
```

**Dangling edge → 422**
```bash
python3 -c "import json;w=json.load(open('wf.json'));w['edges'].append({'from':'u1','to':'ghost'});json.dump(w,open('dangle.json','w'))"
curl -s -X PUT localhost:8080/api/workflows/wf_preview_envs \
  -H 'Content-Type: application/json' --data @dangle.json -w "\nHTTP %{http_code}\n"
# {"details":["edge to unknown node \"ghost\""],"error":"workflow is invalid"}
# HTTP 422
```

Other rejected cases (try them the same way): self-loop, duplicate node id,
duplicate edge, unknown component `type`, invalid `kind`, invalid edge `label`,
empty `name`. A valid edit returns **200** and the stored workflow is updated.

In the UI, a rejected save shows the reason in a toast
(`workflow is invalid: ...`) and does not overwrite the stored workflow.

Cleanup:
```bash
rm -f wf.json cycle.json dangle.json
```

---

## 10. Persistence & durability

In-memory mode resets on restart. With Postgres, workflows and runs survive:

```bash
# start Postgres (see §4) on :5433, then:
export CONDUIT_DATABASE_URL="postgres://conduit:conduit@localhost:5433/conduit?sslmode=disable"
make ui && go build -o conduitd .
./conduitd &                       # logs "persistence: postgres"
# run + approve a workflow (see §7), note the run id, then:
kill %1                            # stop the server
./conduitd &                       # start fresh
curl -s localhost:8080/api/runs | python3 -c "import sys,json;print([r['id'] for r in json.load(sys.stdin)])"
# the previous run id is still listed — state survived the restart
kill %1
```

The schema is created automatically on first connect (tables `workflows` and
`runs`, JSONB-backed). No migration tool is required.

---

## 10b. Phase 1 features (retries, cancel, webhooks, live HTTP)

With the server running (`make up` or `make run`):

**Cancel a run**
```bash
RID=$(curl -s -X POST localhost:8080/api/runs -H 'Content-Type: application/json' \
  -d '{"workflow_id":"wf_preview_envs"}' | python3 -c "import sys,json;print(json.load(sys.stdin)['id'])")
sleep 1
curl -s -X POST localhost:8080/api/runs/$RID/cancel -w "\nHTTP %{http_code}\n"
curl -s localhost:8080/api/runs/$RID | python3 -c "import sys,json;print(json.load(sys.stdin)['status'])"
# -> canceled   (a second cancel returns HTTP 409)
```
In the UI, click **Run**, then the **Cancel** button that replaces it.

**Trigger a run via webhook** (the body becomes the trigger's `input`)
```bash
curl -s -X POST localhost:8080/api/hooks/wf_preview_envs \
  -H 'Content-Type: application/json' -d '{"pr":154,"action":"opened"}' | python3 -m json.tool
# then inspect the trigger node's output:
#   runs/{id} -> steps.t1.output.input == {"pr":154,"action":"opened"}
```

**Retries**: add `retries` and `retry_backoff_ms` fields to a node's config (in
the UI's Config tab or via `PUT`). A failing node is retried that many times
before the `failed` edge is taken; the Console logs each `attempt N failed,
retrying`.

**Real HTTP**: set a node's config `live: true` with a `url` (and optional
`method`, `body`, `content_type`). The executor makes a real request and records
`status_code`, `latency_ms`, and a body snippet; 2xx → success, else failed. The
seeded workflow leaves `live` unset, so it stays simulated and offline.

These behaviors are covered by `go test ./...` (engine retries/cancel/webhook,
the live HTTP executor against an `httptest` server, and the cancel/hook API
endpoints).

## 10c. Phase 2 features (parallel, scheduling, history)

**Parallel execution** — independent branches run concurrently. In the seeded
workflow, after Health Check the Memory-write and Approval branches run at the
same time; you can see both advance together in the Console/canvas during a run.

**Cron scheduling** — add a `schedule` trigger with a `cron` field, then list
active schedules:
```bash
curl -s -X PUT localhost:8080/api/workflows/wf_cron -H 'Content-Type: application/json' -d '{
  "id":"wf_cron","name":"Cron demo",
  "nodes":[{"id":"t","type":"schedule","title":"Every minute","icon":"clock","kind":"trigger","x":0,"y":0,"config":[{"key":"cron","value":"@every 1m"}]},
           {"id":"s","type":"status","title":"Done","icon":"flag","kind":"status","x":300,"y":0,"config":[]}],
  "edges":[{"from":"t","to":"s"}]}'
curl -s localhost:8080/api/schedules
# [{"workflow_id":"wf_cron",...,"cron":"@every 1m"}]
```
Cron syntax: standard 5-field, an optional leading seconds field, or descriptors
like `@every 30s` and `@hourly`. A run appears in `GET /api/runs` each time it fires.

**Run history** — in the UI, click **Run history** in the left rail to list past
runs; pick one to load its steps, outputs and memory. Over the API it's just
`GET /api/runs` and `GET /api/runs/{id}`.

**New components** — `filter` stops a branch (without failing the run) when its
`result` is `false`; `merge` is a pass-through join; `graphql` mirrors the
simulated HTTP node. All selectable from the Edit palette.

## 10d. Phase 3 features (secrets & connectors)

Set `CONDUIT_SECRET_KEY` so secrets persist (otherwise an ephemeral key is used):

```bash
CONDUIT_SECRET_KEY=my-passphrase ./conduitd   # or set it in docker-compose.yml
```

**Manage secrets** (values are write-only — never returned):
```bash
curl -s -X PUT localhost:8080/api/secrets/slack_url \
  -H 'Content-Type: application/json' -d '{"value":"https://hooks.slack.com/services/XXX"}'
curl -s localhost:8080/api/secrets          # ["slack_url"]
curl -s -X DELETE localhost:8080/api/secrets/slack_url
```
In the UI, click **Secrets** in the top bar to add/list/delete.

**Use a secret in a node** — reference it as `${secret:NAME}` in any config value.
A `slack` node posts to its `webhook` (typically `${secret:slack_url}`); a live
`http` node (`live: true`) can send an `authorization` header from a secret. The
executor expands the reference at run time; the secret value never appears in the
workflow graph.

**End-to-end check** (points a Slack node at the app's own webhook endpoint,
which returns 201, to exercise real resolution + POST without an external
service):
```bash
curl -s -X PUT localhost:8080/api/secrets/slack_url -H 'Content-Type: application/json' \
  -d '{"value":"http://127.0.0.1:8080/api/hooks/wf_preview_envs"}'
curl -s -X PUT localhost:8080/api/workflows/wf_slack -H 'Content-Type: application/json' -d '{
  "id":"wf_slack","name":"Notify",
  "nodes":[{"id":"t","type":"webhook","title":"Start","icon":"webhook","kind":"trigger","x":0,"y":0,"config":[]},
           {"id":"n","type":"slack","title":"Notify","icon":"webhook","kind":"step","x":300,"y":0,"config":[{"key":"webhook","value":"${secret:slack_url}"},{"key":"text","value":"hi"}]}],
  "edges":[{"from":"t","to":"n"}]}'
RID=$(curl -s -X POST localhost:8080/api/runs -H 'Content-Type: application/json' -d '{"workflow_id":"wf_slack"}' | python3 -c "import sys,json;print(json.load(sys.stdin)['id'])")
sleep 2; curl -s localhost:8080/api/runs/$RID | python3 -c "import sys,json;r=json.load(sys.stdin);print(r['steps']['n']['status'], r['steps']['n']['output'].get('status_code'))"
# -> success 201
```

These are covered by `go test ./...`: secrets manager (encrypt/decrypt, wrong
key), the store secret contract (Mem + Postgres), the Slack executor and secret
expansion (via `httptest`), and the `/api/secrets` endpoints.

## 10e. Phase 4a features (auth & RBAC)

When auth is enabled (it always is via the binary/compose; tests run in open
mode), every `/api` route needs a token except health, login, and the UI.

A bootstrap admin is created on first run — default `admin@conduit.local` /
`conduit` (override with `CONDUIT_ADMIN_EMAIL` / `CONDUIT_ADMIN_PASSWORD`; set
`CONDUIT_JWT_SECRET` so sessions survive restarts).

```bash
# unauthenticated read is rejected
curl -s -o /dev/null -w "%{http_code}\n" localhost:8080/api/workflows      # 401

# log in to get a JWT
TOK=$(curl -s -X POST localhost:8080/api/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"admin@conduit.local","password":"conduit"}' | python3 -c "import sys,json;print(json.load(sys.stdin)['token'])")

curl -s -H "Authorization: Bearer $TOK" localhost:8080/api/auth/me          # current user
curl -s -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $TOK" localhost:8080/api/workflows  # 200

# create an API token (for CI / webhooks); plaintext is shown once
curl -s -X POST localhost:8080/api/tokens -H "Authorization: Bearer $TOK" \
  -H 'Content-Type: application/json' -d '{"name":"ci","role":"member"}'

# webhooks authenticate with an API token
curl -s -X POST "localhost:8080/api/hooks/wf_preview_envs?token=<cdt_...>" \
  -H 'Content-Type: application/json' -d '{}'
```

**RBAC**: a `viewer` can read but not mutate (writes return `403`). In the UI,
sign in on the login screen; the top bar shows the user and a sign-out button.

Covered by `go test ./...`: the auth service (bcrypt login, JWT round-trip +
tamper, bootstrap, API-token lifecycle) and the API middleware (401 unauth, 403
viewer-write, 200 admin, webhook token requirement, login endpoint).

## 10f. Phase 4b features (projects / multi-tenancy)

Workflows and runs are scoped to a project, chosen with the `X-Conduit-Project`
header (default `default`). With a JWT in `$TOK`:

```bash
# the seeded workflow lives in the default project
curl -s -H "Authorization: Bearer $TOK" localhost:8080/api/projects
curl -s -H "Authorization: Bearer $TOK" localhost:8080/api/workflows | python3 -c "import sys,json;print([w['id'] for w in json.load(sys.stdin)])"

# create a project
curl -s -X POST -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"name":"Staging"}' localhost:8080/api/projects

# that project is isolated — empty, and the seed is 404 under it
curl -s -H "Authorization: Bearer $TOK" -H "X-Conduit-Project: staging" localhost:8080/api/workflows          # []
curl -s -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $TOK" \
  -H "X-Conduit-Project: staging" localhost:8080/api/workflows/wf_preview_envs                                 # 404
```

In the UI, use the **project switcher** (top bar) to change projects or create a
new one; a fresh project opens with a starter workflow. Covered by
`go test ./...` (store project scoping in the contract test, and an API isolation
test).

## 10g. Phase 4c features (membership & scoped secrets)

Projects now isolate by **user**: a caller must be a member of the selected
project (global owner/admin override). With an admin JWT in `$TOK`:

```bash
# create a user (admin only)
curl -s -X POST -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"email":"bob@x.com","name":"Bob","password":"pw","role":"member"}' localhost:8080/api/users

# bob logs in and is NOT yet a member of the default project -> 403
BTOK=$(curl -s -X POST localhost:8080/api/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"bob@x.com","password":"pw"}' | python3 -c "import sys,json;print(json.load(sys.stdin)['token'])")
curl -s -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $BTOK" localhost:8080/api/workflows   # 403

# admin adds bob to default -> bob can now read it
curl -s -X POST -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"email":"bob@x.com","role":"member"}' localhost:8080/api/projects/default/members
curl -s -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $BTOK" localhost:8080/api/workflows   # 200
```

**Secrets are project-scoped** — set under one project, invisible under another:
```bash
curl -s -X PUT -H "Authorization: Bearer $TOK" -H "X-Conduit-Project: default" \
  -H 'Content-Type: application/json' -d '{"value":"v1"}' localhost:8080/api/secrets/mykey
curl -s -H "Authorization: Bearer $TOK" -H "X-Conduit-Project: default" localhost:8080/api/secrets   # ["mykey"]
curl -s -H "Authorization: Bearer $TOK" -H "X-Conduit-Project: staging" localhost:8080/api/secrets   # []
```

In the UI, use the **Members** button (top bar) to add/remove members or create
users. Covered by `go test ./...` (store member + scoped-secret contract, and API
membership isolation — non-member 403, member 200).

## 10h. Phase 5 features (realtime, scale & ops)

**Metrics** — Prometheus text at `/metrics` (public, not under `/api`):
```bash
curl -s localhost:8080/metrics | grep conduit_
# conduit_http_requests_total{method="GET",status="2xx"} ...
# conduit_runs_started_total 1, conduit_runs_finished_total{status="success"} 1, conduit_runs_active 0
```

**Audit log** — mutations are recorded; read them as a global admin:
```bash
curl -s -H "Authorization: Bearer $TOK" localhost:8080/api/audit | python3 -m json.tool
# entries show actor, method, path, project, status
```

**Structured logs** — the server logs one JSON line per request (with a request
id, returned in the `X-Request-ID` header):
```
{"level":"INFO","msg":"request","id":"...","method":"POST","path":"/api/runs","status":201,"dur_ms":0}
```

**Rate limiting** — per-IP token bucket on `/api`; over the limit returns `429`
with `Retry-After`. Tune with `CONDUIT_RATE_LIMIT` (req/sec, 0 disables).

**Multi-instance live events (Postgres)** — with `CONDUIT_DATABASE_URL` set, run
events fan out across instances via `LISTEN/NOTIFY`, so SSE works behind a load
balancer. To exercise the relay directly against a DB:
```bash
export CONDUIT_TEST_POSTGRES="postgres://user:pass@localhost:5432/conduit?sslmode=disable"
go test ./internal/store/ -run TestPostgresEventRelay -v   # publishes on one instance, receives on another
```

Covered by `go test ./...`: metrics exposition, rate-limit 429, audit record +
`/api/audit`, the store audit contract, and the gated cross-instance relay.

## 10i. Phase 5b features (worker model)

Runs are now **enqueued** and executed by a worker pool, rather than inline.

```bash
# enqueue a run — it comes back queued, then a worker picks it up
curl -s -X POST -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"workflow_id":"wf_preview_envs"}' localhost:8080/api/runs
# -> {"id":"run_...","status":"queued",...}
# poll GET /api/runs/{id}: status moves queued -> running -> waiting (approval) -> success
```

- The server logs `worker pool starting` with the pool size (`CONDUIT_WORKERS`,
  default 4).
- With Postgres, claims use `SELECT … FOR UPDATE SKIP LOCKED`, so running several
  instances against one database never double-executes a run; a crashed worker's
  lease expires and another reclaims it.

Covered by `go test ./...`: the worker executes a queued run (`internal/worker`),
an expired lease is reclaimable, the store claim/lease contract (Mem + real
Postgres `SKIP LOCKED`), and the API run lifecycle runs through the worker pool.

## 10j. Phase 6 features (agents / AI factory)

The `agent` component prompts an LLM. It is **simulated by default** (offline,
deterministic), so the seeded factory runs without a provider.

The **AI Factory** sample lives in the `ai-factory` project (switch to it in the
UI's project switcher, or send `X-Conduit-Project: ai-factory`):

```bash
# run the factory (agent steps simulate: refine -> implement -> verify -> review)
RID=$(curl -s -X POST -H "Authorization: Bearer $TOK" -H "X-Conduit-Project: ai-factory" \
  -H 'Content-Type: application/json' -d '{"workflow_id":"wf_factory"}' localhost:8080/api/runs \
  | python3 -c "import sys,json;print(json.load(sys.stdin)['id'])")
sleep 6
curl -s -H "Authorization: Bearer $TOK" -H "X-Conduit-Project: ai-factory" localhost:8080/api/runs/$RID \
  | python3 -c "import sys,json;r=json.load(sys.stdin);print(r['status'], r['steps']['review']['status'])"
# -> waiting success   (paused at Human Approval)

# approve to finish (opens PR, records delivery)
curl -s -X POST -H "Authorization: Bearer $TOK" -H "X-Conduit-Project: ai-factory" \
  -H 'Content-Type: application/json' -d '{"node_id":"appr","decision":"approved"}' \
  localhost:8080/api/runs/$RID/approve
```

**Live agent** — to call a real model, add to an agent node's config: `live: true`,
`provider` (`anthropic` / `openai` / `custom`), `model`, and
`api_key: ${secret:NAME}` (store the key as a project secret). Prompts may
reference run state with `${input.key}` and `${memory.key}`.

Covered by `go test ./...`: the simulated agent, live agent via `httptest`
(Anthropic + OpenAI request/response shapes, failure status), prompt templating,
and that the seeded factory workflow passes validation.

## 10k. Phase 7 features (hardening)

**SSRF guard** — outbound executors (`http` live, `slack`, `agent`) refuse
private/loopback/link-local targets at dial time. With the server running
normally (guard on), a live HTTP node pointed at `127.0.0.1`/`10.x`/`169.254.x`
fails. For local testing against internal targets, start with
`CONDUIT_ALLOW_PRIVATE_EGRESS=true`.

**Security headers** — every response carries them:
```bash
curl -s -D - -o /dev/null localhost:8080/ | grep -iE 'x-content-type-options|x-frame-options|referrer-policy'
# X-Content-Type-Options: nosniff / X-Frame-Options: SAMEORIGIN / Referrer-Policy: no-referrer
```

**Body-size cap** — requests over 4 MiB are rejected instead of buffered.

Covered by `go test ./...`: the SSRF guard blocks loopback when disallowed and
permits it when allowed (`internal/components`); the rest is middleware exercised
by the API suite.

See **[SECURITY.md](SECURITY.md)** for the full security model and the production
checklist.

## 10l. Going real — live execution against external systems

The seeded demo workflows **simulate** their side effects so they run offline and
deterministically. To exercise Conduit end-to-end against real systems, components
have live modes:

| Component | How to make it real |
|-----------|---------------------|
| `http` / `graphql` | set field `live = true` (performs a real request; outcome from the status code) |
| `slack` | set `webhook = ${secret:slack_url}` (always posts for real) |
| `agent` | set `live = true`, `provider`, `model`, `api_key = ${secret:...}` |
| `github_pr` | opens a **real pull request** (always real; needs `github_token`) |
| `github_comment` | posts a **real** issue/PR comment (needs `github_token`) |

### Open a real GitHub pull request

This is the "configure an actual PR for a git repo" path.

**1. Create a GitHub token.** On GitHub → *Settings → Developer settings →
Personal access tokens → Fine-grained tokens*. Scope it to the target repository
with **Pull requests: Read and write** (and **Contents: Read and write** if the
`head` branch is created by an earlier step). Copy the token.

**2. Store it as a project secret** named `github_token` (UI → **Secrets**, or API):

```bash
curl -s -X PUT localhost:8080/api/secrets/github_token \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"value":"github_pat_xxx"}'
```

**3. Add a `github_pr` node** (palette → **GitHub PR**) and configure its fields:

| Field | Example | Notes |
|-------|---------|-------|
| `owner` | `your-org` | repo owner |
| `repo` | `your-repo` | repo name |
| `head` | `feature/login` or `${input.branch}` | the branch with changes (must already exist on the remote) |
| `base` | `main` | target branch (default `main`) |
| `title` | `Automated: ${input.branch}` | supports `${input.*}` / `${memory.*}` |
| `body` | `Opened by Conduit` | optional |
| `token` | *(omit)* | defaults to `${secret:github_token}` |
| `api_base` | *(omit)* | set to your GHE API URL for GitHub Enterprise |

Config values expand `${secret:NAME}`, `${input.KEY}` (the webhook/run payload)
and `${memory.KEY}` (state written by earlier steps).

**4. Run it.** On success the step is **Created** and its output carries the real
`number` and `html_url`; the PR URL is also written to memory as `last_pr_url`.

**Verify the real call fires even without a valid token** (it reaches GitHub and
returns a real error — proof it is not simulated):

```bash
# with no github_token secret -> failed, with a hint
# with a bogus github_token   -> failed, status_code 401 "Bad credentials"
curl -s localhost:8080/api/runs/<run_id> -H "Authorization: Bearer $TOK" \
  | python3 -c "import sys,json;print(json.load(sys.stdin)['steps']['<node_id>'])"
```

A full webhook-driven recipe (Git host → run → real PR) is in
**[GUIDE.md](GUIDE.md)**.

## 10m. Run control (cancel & approval)

**Cancel** stops a run in any non-terminal state and is **idempotent**:

- Canceling a **running** or **waiting** run unwinds it to `canceled`.
- Canceling a **queued** run (before a worker claims it) marks it `canceled` and a
  tombstone prevents a worker from executing it.
- Canceling an **already-finished** run returns `200` with its status (so the UI
  never gets stuck showing "Cancel"); an unknown run returns `404`.

```bash
# start a run, let it reach `waiting`, then cancel it
curl -s -X POST localhost:8080/api/runs/<id>/cancel -H "Authorization: Bearer $TOK" \
  -H 'Content-Type: application/json' -d '{}'
# -> {"ok":true,"canceled":true,"status":"waiting"}  (settles to canceled)
```

**Approval** emits a `run.waiting` SSE event and, in the UI, a **persistent banner**
across the top with **Approve / Reject / View step** (plus a best-effort desktop
notification), so a held run is impossible to miss regardless of which tab you are
on. Resolve it from the banner, or via the API:

```bash
curl -s -X POST localhost:8080/api/runs/<id>/approve -H "Authorization: Bearer $TOK" \
  -H 'Content-Type: application/json' -d '{"node_id":"<approval_node>","decision":"approved"}'
```

If the SSE stream drops, the UI reconciles from `GET /api/runs/<id>`, so the
Run/Cancel button and the approval banner always match the backend's real state.

## 11. Docker

Full stack with Postgres (the recommended way to run and test):

```bash
make up          # docker compose up --build  — app + UI + Postgres
make logs        # tail logs
make down        # stop (keep data)
make down-v      # stop and drop the database volume
```

The app container has a built-in healthcheck (`conduitd healthcheck`), so
`docker compose ps` shows it as `healthy` once it is serving.

Single container, no database (in-memory, resets on exit):

```bash
docker build -t conduit .
docker run --rm -p 8080:8080 conduit
```

---

## 12. Troubleshooting

| Symptom | Cause / fix |
|---------|-------------|
| `pattern all:web/out: no matching files` on `go build` | The export placeholder is missing. Run `make ui`, or `mkdir -p web/out && touch web/out/.gitkeep`. |
| UI loads but `/` returns 404 | `web/out` has only the placeholder — run `make ui` to build the real UI. |
| `go test` hangs on an engine test | A run is waiting on approval; engine tests zero the step delays and resolve gates themselves, so re-run `go test ./internal/engine/ -v` to see which case. |
| Postgres test fails to connect | Check `CONDUIT_TEST_POSTGRES` host/port and that the container is healthy (`docker logs conduit-pg`). |
| SSE stream shows nothing | Confirm the run id is correct and the run has not already finished; omit `?run=` to see all events. |
| `next dev` can't reach the API | Set `NEXT_PUBLIC_API_BASE=http://localhost:8080` (the `make run-dev` target does this) and keep the Go server running. |
