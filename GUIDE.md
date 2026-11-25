# Conduit Agent — Getting Started Guide

This guide takes you from zero to running Conduit Agent (locally or with Docker),
then walks through it the way you would actually use it: a ticket goes in and a
reviewed pull request comes out.

**Conduit Agent is an open-source, self-hosted AI software factory.** Agents triage
a ticket, plan the change and write the code on a branch; you approve the plan and
the exact files; CI has to pass; then a pull request opens and goes to review.
Every step is a node on a visual graph you can watch live, skip, or re-run from
where it failed. Underneath is a general workflow engine, so the same canvas also
builds other event-driven automations, with secrets, approvals, metrics and an
audit trail, all from one binary.

- [1. Run it locally (from source)](#1-run-it-locally-from-source)
- [2. Run it with Docker](#2-run-it-with-docker)
- [3. First sign-in](#3-first-sign-in)
- [4. The AI Factory: ticket → reviewed PR](#4-the-ai-factory-ticket--reviewed-pr)
- [5. Run control: approve, skip, re-run, cancel](#5-run-control-approve-skip-re-run-cancel)
- [6. Beyond the factory: PR preview environments](#6-beyond-the-factory-pr-preview-environments)
- [7. Trigger it the way production does: a webhook](#7-trigger-it-the-way-production-does-a-webhook)
- [8. Make it real: open an actual GitHub pull request](#8-make-it-real-open-an-actual-github-pull-request)
- [9. Build your own automation from scratch](#9-build-your-own-automation-from-scratch)
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

## 3. First sign-in

Open **http://localhost:8080** and choose **Sign in** or **Get started**.

- **Google or GitHub** sign-in appears when the server has those OAuth providers
  configured; **email and password** sign-up always works (unless
  `CONDUIT_ALLOW_SIGNUP=false`).
- **Every account gets its own workspace:** two projects you own,
  *"&lt;your name&gt; · AI Factory"* and *"&lt;your name&gt; · GitHub"*, each holding your
  own copy of the built-in workflow. Your settings and secrets live in those
  projects, are encrypted at rest, and are not visible to other users.
- **The server's administrator** signs in with the bootstrap admin account,
  created on first run (`CONDUIT_ADMIN_EMAIL` / `CONDUIT_ADMIN_PASSWORD`, default
  `admin@conduit.local` / `conduit`). The admin sees every project, including the
  shared *Default* project with the *"Preview Environments · DigitalOcean"* example.

Once signed in, the top bar has the **project picker**, the workflow name,
**Run / Cancel** and **Edit**, plus icon buttons for **Members**, **Settings**
(variables and secrets) and the theme. Below it, switch between **Workflow**,
**Board** and **Metrics**; in the Workflow view, the **Canvas / Console / Memory /
Files** tabs, with the steps of the current run listed in the left rail.

> If you run the server for anyone but yourself, change the admin password and
> set your own `CONDUIT_JWT_SECRET` / `CONDUIT_SECRET_KEY` first — see
> [SECURITY.md](SECURITY.md). Settings and secrets only survive a restart when
> `CONDUIT_SECRET_KEY` is set and the server uses Postgres.

---

## 4. The AI Factory: ticket → reviewed PR

Open your **AI Factory** project (project picker). Its workflow,
*"AI Factory · Ticket → PR"*, is the whole pipeline:

```
Ticket (board task / Jira / Notion / webhook)
  → Triage · Confidence ──low──▶ Needs clarification (a person, not a model)
  → Refine & Plan → Draft Implementation → Review Confidence   (agents)
  → Human Approval            ← gate 1: the plan
  → Plan Changes → Approve Changes   ← gate 2: the exact files
  → Apply Changes  (writes the files — nothing is pushed yet)
  → Review Diff                      ← gate 3: the actual code
  → Push Branch    (pushes exactly what you approved, via the GitHub API)
  → Verify Checks (CI) ──fail──▶ back to Apply with the failures, up to twice
  → Open Pull Request → Post PR to Ticket → Record Delivery
```

**1. Configure it once.** Open **Settings** and add, at minimum:

| Kind | Name | Example |
| --- | --- | --- |
| Variable | `owner`, `repo` | `acme`, `web` — the repository to change |
| Variable | `llm_provider`, `llm_model` | `anthropic`, your model ID (or `openai` / `custom`) |
| Secret | `github_token` | a token with **contents: write** and **pull requests: write** |
| Secret | `llm_key` | your model provider's API key |

Next, **Settings → Project context**: describe the stack, the build and test
commands, conventions and anything off-limits. Every agent reads it, so plans and
code follow your project instead of generic defaults.

Then open **Settings → Coding agent** and choose how the code gets written:
**Claude Code** (recommended — it reads your whole repository, edits files in a real
checkout and can run your tests; signs in with your **Claude subscription** token from
`claude setup-token`, or an **Anthropic API key**) or **Direct API** (one model call per
file, with the `llm_*` settings above). **Save & test connection** checks the sign-in
before you run anything. Details: README,
[Choose how code is written](README.md#choose-how-code-is-written-claude-code-or-the-direct-api).

Jira or Notion as the ticket source need their own settings (`jira_base`,
`jira_email`, `jira_token`, …); the full table is in the README's
[AI Factory section](README.md#the-ai-factory--ticket--reviewed-pr).

**2. Give it a ticket.** Open **Board → New task**. Write the problem and, ideally,
a checkable outcome — for example *"Add SEO metadata, sitemap.xml and robots.txt.
Done when Lighthouse SEO passes."* Clear tickets score higher at triage. The card
lands in **Backlog**; click **Start**.

**3. Watch and approve.** The run opens on the canvas and each node moves from
*yet to start* to *in progress* to *done* live.

- At **Human Approval**, a banner appears with what the agents suggested: the plan
  and the review. **Approve** it, **Send back** with a note on what to change (the
  plan is redone with your note and you review it again), or **Reject** to stop the run.
- At **Approve Changes**, you see the exact files the coding agent will create or
  modify, and approve the list.
- At **Review Diff**, press **Review changes** to read the actual code, file by
  file. **Approve & push** and Conduit pushes exactly that change to a branch;
  **Send back** has the agent redo it with your note. Nothing reaches your
  repository before this approval. (Turn on the node's **skip** switch if a project
  doesn't need this step.)
- **Verify Checks** waits for your repository's CI on that commit. A failing check
  goes back to the coding agent as feedback, and the fix comes back to Review Diff.
- **Open Pull Request** opens the PR and posts it to the ticket. The board card
  moves through **Implement → Verify → Done**, with the branch and PR link on it.

**4. Review the PR.** The pull request is handed to your **GitHub** project's
review inbox. Switch to that project and press **Run review** on the banner: an
agent reviews the diff, a security scan runs, and a person approves before
Conduit posts the review and merges.

> **No credentials yet?** Without `llm_key`, triage falls back to an offline
> heuristic, and you can skip any step you are not ready to run (see §5). The
> README's [smoke test](README.md#4-try-it-endtoend-smoke-test-no-credentials)
> walks the whole shape offline.

Tickets can also arrive automatically: point GitHub issues or Jira at the intake
webhook and they land in Backlog, scored, optionally auto-starting above a
confidence threshold (README, *Run it — two ways*).

---

## 5. Run control: approve, skip, re-run, cancel

- **Approve / Send back / Reject.** When a run reaches an approval gate, a
  **persistent banner** appears across the top of the app with what the agent
  suggested, plus a best-effort desktop notification, so a held run is hard to miss.
  (Allow notifications when your browser asks.) **Send back** asks what should
  change; the step that made the proposal redoes it with your note and its previous
  answer, and the banner returns with *Your last feedback* so you can check it was
  addressed. Each gate allows 3 send-backs; after that, sending back counts as a
  rejection.
- **Skip a step.** Each node has a **skip** switch. A skipped step is bypassed on
  the next run — handy for *Verify Checks (CI)* on a repository without CI, or for
  a token that cannot read check-runs. The change saves immediately.
- **Re-run from a failed stage.** Select the failed node and press **Re-run from
  here**: the run continues from that stage and keeps the work that already succeeded.
- **Cancel** (top bar, while a run is active) stops it at the next step boundary,
  whether it is queued, executing, or waiting on a person, and the button returns
  to **Run**. It is idempotent and the UI re-syncs with the server.

---

## 6. Beyond the factory: PR preview environments

**The problem.** Your team wants every pull request to get its own disposable
preview environment — provision a server, deploy the branch, health‑check it, and
post the URL on the PR — but you don't want deploys firing outside business hours
or without an on‑call sign‑off, and you want a record of what was created so it can
be cleaned up later.

The same engine runs this without any AI: it is the **Preview Environments**
example in the shared *Default* project (visible to the admin; anyone can build
it with §9). Here's the graph:

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
> §8 and §9 show how to make steps perform *real* actions (a live GitHub PR, a
> real HTTP/Slack call, a live LLM).

---

## 7. Trigger it the way production does: a webhook

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

## 8. Make it real: open an actual GitHub pull request

The seeded demo is simulated; this section does something **real** against GitHub,
end to end. We'll build *"on webhook → open a pull request in my repo."*

> **Shortcut:** a ready-made version ships in the **GitHub PR** project (pick it in
> the top-bar project switcher) — the *"Open a real PR · GitHub"* workflow, wired
> `Webhook → Review & Approve → Open Pull Request`. Just set the `github_token`
> secret and edit the PR node's `owner`/`repo`, then run it. The steps below explain
> each piece (and how to build it yourself).

**a) Create a GitHub token.** GitHub → *Settings → Developer settings → Personal
access tokens → Fine-grained tokens*. Give it access to your repo with
**Pull requests: Read and write** (add **Contents: Read and write** if an earlier
step creates the `head` branch). Copy it.

**b) Store it as a project secret** named `github_token` (top bar → **Settings** → Secrets):

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

## 9. Build your own automation from scratch

Let's build something genuinely useful and **make one step do a real thing**:
*"When called, run a health check against a real URL and post the result to Slack;
if the check fails, still notify."* Secrets keep the Slack URL out of the graph.

**a) Store the Slack webhook as a secret** (top bar → **Settings** → Secrets, or API):

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

## 10. Why this saves you time

Without Conduit, turning a well-written ticket into a pull request means someone
reads it, plans it, writes the change, waits for CI, fixes what broke and opens the
PR. With Conduit, the agents do the typing while a person keeps every decision:

- **Three approval gates** on every change: the plan, the exact files, then the
  actual diff. Nothing reaches your repository without a person approving the code.
- **Triage first**, so vague tickets go to a person instead of producing a
  confident-looking wrong change.
- **CI has to pass**, with failures fed back to the coding agent automatically.
- **A live, visual pipeline** — every step's status, output and logs as it happens,
  with skip, re-run-from-failure and cancel.
- **Your model, your repository, your server** — Anthropic, OpenAI or your own
  gateway; self-hosted; per-user encrypted settings and secrets.
- **One engine for the rest**: webhooks, cron schedules, HTTP and Slack steps,
  conditions and approvals for any other automation, with RBAC, metrics
  (`/metrics`) and an audit log, from one binary and one `make up`.

Next: the full command/endpoint reference is in [README.md](README.md), the
reproducible test guide in [TESTING.md](TESTING.md), the security model and
production checklist in [SECURITY.md](SECURITY.md), and what comes next in
[ROADMAP.md](ROADMAP.md).
