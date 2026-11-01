# Security

## Reporting

Please report vulnerabilities privately to the maintainers (open a minimal issue
asking for a security contact, or use the repository's private advisory feature).
Do not file public issues with exploit details.

## Security model

- **Authentication**: password login (bcrypt) issues short-lived HS256 **JWTs**
  signed with `CONDUIT_JWT_SECRET`; **API tokens** are random and stored only as
  SHA-256 hashes.
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
