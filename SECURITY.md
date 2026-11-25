# Security

## Reporting

Please report vulnerabilities privately to the maintainers (open a minimal issue
asking for a security contact, or use the repository's private advisory feature).
Do not file public issues with exploit details.

## Security model

- **Authentication**: password login (bcrypt) issues short-lived HS256 **JWTs**
  signed with `CONDUIT_JWT_SECRET`; **API tokens** are random and stored only as
  SHA-256 hashes. Optional **OAuth sign-in** (Google / GitHub) uses the
  authorization-code flow with a signed, short-lived `state` (CSRF) parameter; the
  JWT is returned to the SPA in the URL **fragment** (never sent to a server or
  logged). External identities are persisted in a `user_identities` table
  (`(provider, subject)` unique) that links a provider login to a Conduit user.
- **Authorization (RBAC)**: roles are owner / admin / member / viewer. Every
  `/api` route requires authentication except `GET /api/health`,
  `POST /api/auth/login`, and the static UI. Write methods require a non-viewer
  role.
- **Multi-tenancy**: workflows, runs and secrets are scoped to a **project**.
  A user may act in a project only if they are a **member** (global owner/admin
  override); non-members get 403, cross-project reads 404.
- **Secrets**: encrypted at rest with AES-256-GCM (`CONDUIT_SECRET_KEY`); values
  are write-only over the API and resolved per-run, never returned.
- **SSRF protection**: outbound executors (`http` live, `slack`, `agent`) resolve
  the target and **block private/loopback/link-local addresses at dial time**
  (robust to DNS rebinding). Set `CONDUIT_ALLOW_PRIVATE_EGRESS=true` only for
  local testing or a trusted internal network.
- **Request hardening**: per-IP rate limiting (`CONDUIT_RATE_LIMIT`), a 4 MiB
  request-body cap, `nosniff` / `SAMEORIGIN` / `no-referrer` headers, HTTP read
  and idle timeouts, and graceful shutdown.
- **Claude Code coding engine** (`coding_engine=claude_code`): each session runs
  in a throwaway clone with an empty `HOME` and an allowlisted environment (`PATH`,
  locale, proxy/CA settings and the one sign-in variable) — never the server's
  database URL, `CONDUIT_SECRET_KEY`, JWT secret or the GitHub token (Conduit's own
  `git` uses the token via `GIT_CONFIG_*`, not argv or `.git/config`). Permissions
  are deny-by-default (`--permission-mode dontAsk`): reads/edits scoped to the
  checkout, no web tools, no MCP servers, repository `.claude` settings ignored.
  Shell access is off unless a project lists command prefixes in
  `claude_allowed_commands`; `git push` is always denied and pushing is done by
  Conduit after verifying that only approved files changed.
- **Audit log**: every mutating API call is recorded (actor, method, path,
  project, status), readable by a global admin at `GET /api/audit`.

## Production checklist

Before exposing Conduit Agent publicly:

1. Set strong, stable **`CONDUIT_JWT_SECRET`** and **`CONDUIT_SECRET_KEY`**
   (losing them logs everyone out / makes secrets undecryptable).
2. Change the bootstrap admin password (`CONDUIT_ADMIN_PASSWORD`) after first login.
   Decide on self-serve signup: `POST /api/auth/register` is **enabled by default**
   (new users get the `member` role and a personal project). Set
   `CONDUIT_ALLOW_SIGNUP=false` to make accounts admin-provisioned only.
3. Use **Postgres** (`CONDUIT_DATABASE_URL`) and terminate **TLS** at a reverse
   proxy (set `X-Forwarded-For` so rate limiting sees real client IPs).
4. Leave `CONDUIT_ALLOW_PRIVATE_EGRESS` unset unless you specifically need
   internal targets.
5. Tune `CONDUIT_RATE_LIMIT` and `CONDUIT_WORKERS` for your load.
6. Scrape `/metrics` and ship the JSON logs to your log pipeline.

## Known limitations (tracked)

- **Session token storage**: the SPA stores its JWT in `localStorage` (standard
  for a static SPA, but readable by injected scripts). An httpOnly-cookie session
  with CSRF protection is a planned hardening.
- **Reclaim re-runs**: a run whose worker dies is re-executed from the start when
  reclaimed (the engine is not yet resumable). This is safe for idempotent/
  simulated steps; real side-effecting steps should be made idempotent. Resumable
  execution (which also enables cross-instance approval) is planned.
- No OTel tracing yet (metrics + structured logs are in place).
- **Claude Code runs on the Conduit host.** Sessions are isolated as described
  above but are not a container sandbox: an allowed command (e.g. `npm test`)
  executes the repository's own scripts with the server user's privileges and
  network access. Only allow commands for repositories you trust, run Conduit in
  its container, and prefer leaving `claude_allowed_commands` empty (CI still
  tests the pushed branch). A per-session container sandbox is planned.
- **`server_login`** shares the server's single Claude login with every project
  that selects it; enable `CONDUIT_CLAUDE_SERVER_LOGIN` only on a single-user or
  fully trusted self-host. Individual Claude plans are meant for the person who
  holds them — each user should use their own `claude setup-token` token.
