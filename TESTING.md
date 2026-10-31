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

## 6. Manual UI walkthrough

Start the server (`make run`) and open <http://localhost:8080>.

**Run a workflow**
1. Click **Run** (top right). Node badges flip `Pending → Running → Success` and
   the active edge animates.
2. The run pauses at **Require Approval** (badge `Waiting`); a toast says so.
3. Click the **Require Approval** node, then **Approve** in the inspector. The
   run completes; `Publish Preview URL` and `Record Environment` finish.
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
