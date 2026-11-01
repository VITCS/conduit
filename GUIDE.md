# Conduit — Getting Started & Real‑World Guide

This guide takes you from zero to running Conduit (locally and with Docker) and
then **uses it as a real user would**, with a concrete, timed scenario that shows
why it's useful.

Conduit is an **event‑driven automation control plane**: you build a workflow as a
visual graph of components (triggers → steps → status), run it, watch every step
live, gate risky steps on a human, keep secrets and state, and get metrics + an
audit trail — all from one binary.

- [1. Run it locally (from source)](#1-run-it-locally-from-source)
- [2. Run it with Docker](#2-run-it-with-docker)
- [3. First sign‑in](#3-first-sign-in)
- [4. Real‑world walkthrough: PR preview environments](#4-real-world-walkthrough-pr-preview-environments)
- [5. Trigger it the way production does: a webhook](#5-trigger-it-the-way-production-does-a-webhook)
- [6. Make it real: open an actual GitHub pull request](#6-make-it-real-open-an-actual-github-pull-request)
- [7. Build your own automation from scratch](#7-build-your-own-automation-from-scratch)
- [8. The AI Factory: issue → reviewed PR](#8-the-ai-factory-issue--reviewed-pr)
- [9. Run control: cancel and approval](#9-run-control-cancel-and-approval)
- [10. Why this saves you time](#10-why-this-saves-you-time)

---

## 1. Run it locally (from source)

**Prerequisites:** Go 1.24+ and Node 20.9+ (Node only to build the UI; the binary
embeds it and needs no Node at run time).

```bash
git clone https://github.com/VITCS/conduit.git
cd conduit
make ui       # builds the Next.js UI into web/out (npm install + next build)
make run      # go run .  — serves UI + API on http://localhost:8080
```

You'll see structured startup logs, including `persistence: in-memory`,
`worker pool starting`, and `auth: bootstrapped admin "admin@conduit.local"`.
Open **http://localhost:8080**.

> In‑memory mode resets on restart. To persist, set `CONDUIT_DATABASE_URL` (see §2).

Build a single binary instead of `go run`:

```bash
make build    # -> ./conduitd  (UI embedded, one file)
./conduitd
```

## 2. Run it with Docker

The only prerequisite is Docker — no Go, no Node. One command builds the backend
**and** UI into one image and runs it with Postgres:

```bash
make up        # == docker compose up --build  -> http://localhost:8080
```

Helpers:

```bash
make logs      # tail logs
make down      # stop (keep data in the Postgres volume)
make down-v    # stop and wipe the database
```

The compose file sets safe local defaults (`CONDUIT_SECRET_KEY`,
`CONDUIT_JWT_SECRET`, bootstrap admin). For production, change those — see
[SECURITY.md](SECURITY.md).

## 3. First sign‑in

Open **http://localhost:8080**. On first run Conduit creates an admin:

- **Email:** `admin@conduit.local`
- **Password:** `conduit`

Sign in. You land on the **canvas** with a seeded workflow,
*"Preview Environments · DigitalOcean"*. The top bar has a **project switcher**
(Default, AI Factory), **Run / Cancel**, **Members**, **Secrets**, theme, **Edit**,
and your user. The left rail lists the workflow's steps (and, via *Run history*,
past runs). The center is the live graph.

> Change the admin password after first login (create a new owner user from
> **Members → create user**, then sign in as them), and set your own
> `CONDUIT_JWT_SECRET` / `CONDUIT_SECRET_KEY` for anything shared.

---

## 4. Real‑world walkthrough: PR preview environments

**The problem.** Your team wants every pull request to get its own disposable
preview environment — provision a server, deploy the branch, health‑check it, and
post the URL on the PR — but you don't want deploys firing outside business hours
or without an on‑call sign‑off, and you want a record of what was created so it can
be cleaned up later.

That is exactly the seeded **Preview Environments** workflow. Here's the graph:

```
On Pull Request → Business Hours Gate → Provision Server → Configure App → Health Check
                                                                               ├─ success → Record Environment
                                                                               ├─ success → Require Approval → Publish Preview URL
                                                                               └─ failed  → Mark Failed
```

**Run it (as an operator would watch a real deploy):**

1. Click **Run** (top right). Watch the left rail and the graph: each step flips
   `Pending → Running → Success`, and the active edge animates as control flows.
   The **Console** tab shows a structured, timestamped log of every step.
2. The run **pauses at "Require Approval"** with a `Waiting` badge and a toast —
   this is the human gate. Nothing deploys to users until someone signs off.
3. Click the **Require Approval** node. In the inspector click **Approve**
   (or **Reject** to send it down the failure branch). The run continues and
   finishes **Success**.
4. Open the **Memory** tab: the run recorded the environment it created
   (`preview-envs/154 → { droplet_id, url, sha }`) so a later cleanup job can find
   and tear it down.
5. Click any node to inspect its **Details / Payload / Config** — e.g.
   *Provision Server* shows the droplet id, region, size, and IP it "created".

**What you just validated in ~15 seconds:** a multi‑step deploy with a time gate,
a human approval, branch‑on‑failure, live visibility, and durable state — without
writing or babysitting a shell script.

> The seeded steps are **simulated** so the demo runs offline and deterministically.
> §6 and §7 show how to make steps perform *real* actions (a live GitHub PR, a
> real HTTP/Slack call, a live LLM).

---

## 5. Trigger it the way production does: a webhook

In production you don't press "Run" — your Git host does, when a PR opens. Conduit
exposes a webhook per workflow. Webhooks authenticate with an **API token**.

```bash
# 1) sign in to get a session token
TOK=$(curl -s -X POST localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@conduit.local","password":"conduit"}' \
  | python3 -c "import sys,json;print(json.load(sys.stdin)['token'])")

# 2) mint an API token for the webhook sender (CI / Git host)
HOOKTOK=$(curl -s -X POST localhost:8080/api/tokens \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"name":"github-webhook","role":"member"}' \
  | python3 -c "import sys,json;print(json.load(sys.stdin)['token'])")

# 3) fire the webhook as GitHub would, with the PR payload
curl -s -X POST "localhost:8080/api/hooks/wf_preview_envs?token=$HOOKTOK" \
  -H 'Content-Type: application/json' \
  -d '{"number":154,"branch":"fix/inventory-race","sha":"f62f1a9","action":"opened"}'
```

A new run appears (open **Run history** in the left rail, or `GET /api/runs`). The
body you posted is available to the workflow as the trigger's **input** — the
trigger node's output contains `{"input": { ...your JSON... }}`, which downstream
steps can template into their config as `${input.number}`, `${input.branch}`, etc.

This is the real integration path: point your GitHub/GitLab webhook (or a CI step)
at `/api/hooks/{workflowId}?token=…` and every PR drives a run.

---

## 6. Make it real: open an actual GitHub pull request

The seeded demo is simulated; this section does something **real** against GitHub,
end to end. We'll build *"on webhook → open a pull request in my repo."*

**a) Create a GitHub token.** GitHub → *Settings → Developer settings → Personal
access tokens → Fine-grained tokens*. Give it access to your repo with
**Pull requests: Read and write** (add **Contents: Read and write** if an earlier
step creates the `head` branch). Copy it.

**b) Store it as a project secret** named `github_token` (top bar → **Secrets**):

```bash
curl -s -X PUT localhost:8080/api/secrets/github_token \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"value":"github_pat_xxx"}'
```

**c) Build the workflow.** Click **Edit**, then from the palette add a **Webhook**
trigger and a **GitHub PR** step, and wire `Webhook → GitHub PR`. Select the
GitHub PR node and set its config fields:

| Field | Value |
|-------|-------|
| `owner` | your org/user |
| `repo` | your repo |
| `head` | `${input.branch}` (the branch to merge; must exist on the remote) |
| `base` | `main` |
| `title` | `Automated PR: ${input.branch}` |
| `body` | `Opened automatically by Conduit.` |

`token` defaults to `${secret:github_token}`, so you don't need to set it. Values
expand `${secret:NAME}`, `${input.KEY}` (the webhook payload) and `${memory.KEY}`.
Click **Save**.

**d) Fire it** (as your Git host / CI would), passing the branch in the payload:

```bash
HOOKTOK=$(curl -s -X POST localhost:8080/api/tokens \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"name":"github-webhook","role":"member"}' \
  | python3 -c "import sys,json;print(json.load(sys.stdin)['token'])")

curl -s -X POST "localhost:8080/api/hooks/<your_workflow_id>?token=$HOOKTOK" \
  -H 'Content-Type: application/json' -d '{"branch":"feature/login"}'
```

The run executes and the **GitHub PR** step turns **Created** — open it in the UI
and its output shows the real `number` and `html_url`; the PR URL is also saved to
the run's **Memory** as `last_pr_url`. Refresh your repo on GitHub: the pull request
is really there.

> **Honest failure:** if the token is missing the step fails with a clear hint; if
> the token is wrong it reaches GitHub and reports the real error (e.g.
> `401 Bad credentials`) — there's no silent fake success. A **GitHub comment**
> component works the same way for posting a real comment on an issue/PR.

---

## 7. Build your own automation from scratch

Let's build something genuinely useful and **make one step do a real thing**:
*"When called, run a health check against a real URL and post the result to Slack;
if the check fails, still notify."* Secrets keep the Slack URL out of the graph.

**a) Store the Slack webhook as a secret** (top bar → **Secrets**, or API):

```bash
curl -s -X PUT localhost:8080/api/secrets/slack_url \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"value":"https://hooks.slack.com/services/XXX/YYY/ZZZ"}'
```

Secrets are encrypted at rest, **scoped to the current project**, and never
returned by the API — only referenced as `${secret:slack_url}`.

**b) Build the workflow in the UI:**

1. Click **Edit**. From the **Components** palette add a **Webhook** trigger, an
   **HTTP** step, and two **Slack** steps.
2. Drag from each node's right‑hand port to the next to wire them:
   `Webhook → HTTP`, then `HTTP → Slack (ok)` and `HTTP → Slack (alert)`.
3. Click the `HTTP→Slack(alert)` edge and set its **label** to `failed`; leave the
   success edge as `success`.
4. Configure the **HTTP** node (inspector → **Config**): `url =` the service you
   want to check, and `live = true` so it makes a real request.
5. Configure each **Slack** node: `webhook = ${secret:slack_url}` and a `text`
   message (e.g. `Health check passed ✅` / `Health check FAILED ❌`).
6. Click **Save** (writes to `PUT /api/workflows/{id}`, validated — cycles and
   dangling edges are rejected).
7. **Run** it. The HTTP step makes a real call; a 2xx takes the `success` edge to
   the "passed" Slack message, anything else takes the `failed` edge to the alert.

> **Outbound safety:** Conduit blocks requests to private/loopback addresses by
> default (SSRF guard). For a target on your own machine/network during testing,
> start the server with `CONDUIT_ALLOW_PRIVATE_EGRESS=true`.

You now have a real monitor‑and‑alert automation — versioned as a graph, with a
live run view, retries (set `retries` on the HTTP node), and an audit record of
who changed it.

## 8. The AI Factory: issue → reviewed PR

Switch the project (top‑bar switcher) to **AI Factory**. The seeded
*"AI Factory · Issue → Reviewed PR"* workflow models agent‑assisted engineering:

```
On Issue Labeled → Refine & Plan (agent) → Implement (agent) → Verify (CI)
                                                                   ├─ success → Review Confidence (agent) → Human Approval → Open PR → Record
                                                                   └─ failed  → Send Back to Agent
```

Press **Run**: the three **AI Agent** steps produce simulated responses, CI
verifies, and the run pauses at **Human Approval**. Approve it and it "opens a PR"
and records the delivery. To make the agents real, edit an agent node and set
`live = true`, `provider` (`anthropic` / `openai` / `custom`), `model`, and
`api_key = ${secret:my_llm_key}` (store the key in **Secrets** first). Prompts can
pull run context with `${input.issue}` and `${memory.key}`.

To make the final "Open PR" step open a *real* pull request, swap it for a
**GitHub PR** node configured as in §6 — the agents draft the change, the human
approves, and Conduit opens the actual PR.

This shows Conduit coordinating AI agents, CI, and a human reviewer in one visible,
gated pipeline rather than a pile of scripts and manual steps.

## 9. Run control: cancel and approval

- **Cancel** (top bar, while a run is active) stops it immediately and the button
  returns to **Run** — whether the run is queued, executing, or waiting on a human.
  It's idempotent and the UI reconciles with the backend, so it never gets stuck.
- **Approval** raises a **persistent banner** across the top of the app —
  *Approve / Reject / View step* — the moment a run reaches an approval gate, plus a
  best-effort desktop notification, so a held run is impossible to miss no matter
  which tab you're on. (Allow notifications when your browser asks, to get the
  desktop alert too.)

## 10. Why this saves you time

Without Conduit, the §4 pipeline is a bash/CI script: no live view, approvals
hacked in with `sleep`/manual steps, no retry/branch logic, secrets in env files,
no record of what ran or who approved. With Conduit you get, out of the box:

- **A visual, versioned workflow** you can read and change without redeploying code.
- **Live execution** — every step's status, payload, and logs, as it happens.
- **Human gates** (approval) and **branching** (`success` / `failed`) as first‑class.
- **Retries, scheduling (cron), and webhooks** without extra glue.
- **Encrypted, project‑scoped secrets** referenced as `${secret:…}`.
- **Multi‑tenant projects + RBAC**, **metrics** (`/metrics`), and an **audit log**.
- **One binary / one `make up`** to run the whole thing, scaling out with workers.

Next: the full command/endpoint reference is in [README.md](README.md), the
reproducible test guide in [TESTING.md](TESTING.md), and the security model +
production checklist in [SECURITY.md](SECURITY.md).
