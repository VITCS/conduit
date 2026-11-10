// Package api wires HTTP routes to the store and engine. It speaks JSON for the
// REST surface and Server-Sent Events for the live run stream.
package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"conduit/internal/auth"
	"conduit/internal/components"
	"conduit/internal/engine"
	"conduit/internal/metrics"
	"conduit/internal/model"
	"conduit/internal/scheduler"
	"conduit/internal/secrets"
	"conduit/internal/seed"
	"conduit/internal/store"
	"conduit/internal/validate"
)

// Server bundles the dependencies the handlers need.
type Server struct {
	Store     store.Store
	Engine    *engine.Engine
	Scheduler *scheduler.Scheduler // optional; nil disables schedule endpoints/sync
	Secrets   *secrets.Manager     // optional; nil disables secret endpoints
	Auth      *auth.Service        // optional; nil disables auth (open mode, for tests)
	Limiter   *rateLimiter         // optional; nil disables rate limiting
	Web       fs.FS                // static assets (the UI)

	// AllowSignup enables the public self-serve registration endpoint
	// (POST /api/auth/register). When false, accounts are admin-provisioned only.
	AllowSignup bool

	// OAuth holds configured external sign-in providers (google / github), keyed by
	// name. Empty means password auth only — the UI shows no provider buttons.
	OAuth map[string]*OAuthProvider
	// BaseURL is the public origin used to build OAuth redirect URIs, e.g.
	// "https://conduit.example". Empty derives it from the request.
	BaseURL string
}

// NewRateLimiter builds a per-IP token-bucket limiter (requests/second, burst).
func NewRateLimiter(ratePerSec float64, burst int) *rateLimiter {
	return newRateLimiter(ratePerSec, burst)
}

// principal carries the authenticated caller's identity through the request.
type principal struct {
	role   model.Role
	userID string
	email  string
}

type ctxKey int

const principalKey ctxKey = 0

// Routes returns the HTTP handler for the whole app.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /metrics", metrics.Handler())
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/audit", s.listAudit)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/register", s.register)
	mux.HandleFunc("GET /api/auth/config", s.authConfig)
	mux.HandleFunc("GET /api/auth/oauth/{provider}/start", s.oauthStart)
	mux.HandleFunc("GET /api/auth/oauth/{provider}/callback", s.oauthCallback)
	mux.HandleFunc("GET /api/auth/me", s.me)
	mux.HandleFunc("GET /api/tokens", s.listTokens)
	mux.HandleFunc("POST /api/tokens", s.createToken)
	mux.HandleFunc("DELETE /api/tokens/{id}", s.deleteToken)
	mux.HandleFunc("GET /api/components", s.listComponents)
	mux.HandleFunc("GET /api/projects", s.listProjects)
	mux.HandleFunc("POST /api/projects", s.createProject)
	mux.HandleFunc("DELETE /api/projects/{id}", s.deleteProject)
	mux.HandleFunc("GET /api/projects/{id}/members", s.listMembers)
	mux.HandleFunc("POST /api/projects/{id}/members", s.addMember)
	mux.HandleFunc("DELETE /api/projects/{id}/members/{userID}", s.removeMember)
	mux.HandleFunc("GET /api/users", s.listUsers)
	mux.HandleFunc("POST /api/users", s.createUser)
	mux.HandleFunc("GET /api/workflows", s.listWorkflows)
	mux.HandleFunc("GET /api/workflows/{id}", s.getWorkflow)
	mux.HandleFunc("PUT /api/workflows/{id}", s.putWorkflow)
	mux.HandleFunc("GET /api/schedules", s.listSchedules)
	mux.HandleFunc("GET /api/secrets", s.listSecrets)
	mux.HandleFunc("PUT /api/secrets/{name}", s.putSecret)
	mux.HandleFunc("DELETE /api/secrets/{name}", s.deleteSecret)
	mux.HandleFunc("GET /api/vars", s.listVars)
	mux.HandleFunc("PUT /api/vars/{name}", s.putVar)
	mux.HandleFunc("DELETE /api/vars/{name}", s.deleteVar)
	mux.HandleFunc("GET /api/runs", s.listRuns)
	mux.HandleFunc("POST /api/runs", s.createRun)
	mux.HandleFunc("GET /api/runs/{id}", s.getRun)
	mux.HandleFunc("POST /api/runs/{id}/approve", s.approve)
	mux.HandleFunc("POST /api/runs/{id}/cancel", s.cancel)
	mux.HandleFunc("POST /api/runs/{id}/rerun", s.rerun)
	mux.HandleFunc("GET /api/tasks", s.listTasks)
	mux.HandleFunc("POST /api/tasks", s.createTask)
	mux.HandleFunc("GET /api/tasks/{id}", s.getTask)
	mux.HandleFunc("POST /api/tasks/{id}/start", s.startTask)
	mux.HandleFunc("POST /api/tasks/{id}/stage", s.setTaskStage)
	mux.HandleFunc("POST /api/tasks/{id}/notes", s.addTaskNote)
	mux.HandleFunc("DELETE /api/tasks/{id}", s.deleteTask)
	mux.HandleFunc("POST /api/intake/{project}", s.intake)
	mux.HandleFunc("GET /api/metrics", s.boardMetrics)
	mux.HandleFunc("POST /api/hooks/{workflowID}", s.hook)
	mux.HandleFunc("GET /api/events", s.events)

	// Static UI (index.html at /, assets alongside). Content-hashed Next assets
	// under /_next/static are immutable and safe to cache aggressively.
	fileServer := http.FileServer(http.FS(s.Web))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/_next/static/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fileServer.ServeHTTP(w, r)
	}))

	// Middleware chain (outermost first): observe (metrics + log + request id) →
	// security headers → body-size limit → rate limit → auth (sets principal) →
	// audit (records mutations) → mux.
	return observe(secureHeaders(limitBody(s.rateLimit(s.authMiddleware(s.audit(mux))))))
}

// authMiddleware enforces authentication on /api routes when Auth is configured.
// Static assets and a short public allowlist pass through; webhooks require an API
// token; write methods require a non-viewer role.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	if s.Auth == nil {
		return next // open mode (tests)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Non-API paths are the static UI; let them through.
		if !strings.HasPrefix(path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		// Public API endpoints.
		if path == "/api/health" || path == "/api/auth/login" ||
			path == "/api/auth/register" || path == "/api/auth/config" ||
			strings.HasPrefix(path, "/api/auth/oauth/") {
			next.ServeHTTP(w, r)
			return
		}

		// Webhooks authenticate with an API token (bearer or ?token=).
		if strings.HasPrefix(path, "/api/hooks/") || strings.HasPrefix(path, "/api/intake/") {
			tok := bearerToken(r)
			if tok == "" {
				tok = r.URL.Query().Get("token")
			}
			if t, ok := s.Auth.LookupAPIToken(tok); ok {
				next.ServeHTTP(w, r.WithContext(withPrincipal(r, principal{role: t.Role})))
				return
			}
			writeErr(w, http.StatusUnauthorized, "valid API token required")
			return
		}

		// Everything else: JWT or API token.
		p, ok := s.identify(r)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "authentication required")
			return
		}
		// Project-scoped routes require membership of the selected project; the
		// effective role (per-project, or global for admins/tokens) gates writes.
		role := p.role
		if isProjectScoped(path) {
			er, allowed := s.effectiveRole(p, projectID(r))
			if !allowed {
				writeErr(w, http.StatusForbidden, "you are not a member of this project")
				return
			}
			role = er
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !role.CanWrite() {
			writeErr(w, http.StatusForbidden, "your role is read-only")
			return
		}
		p.role = role
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey, p)))
	})
}

// isProjectScoped reports whether a path's authorization depends on project
// membership (as opposed to global, account-level routes).
func isProjectScoped(path string) bool {
	for _, pre := range []string{"/api/workflows", "/api/runs", "/api/secrets", "/api/vars", "/api/schedules"} {
		if path == pre || strings.HasPrefix(path, pre+"/") {
			return true
		}
	}
	return false
}

// effectiveRole resolves the caller's role within a project. API tokens and
// global owner/admin users get access to every project; other users must be a
// member, and their member role applies.
func (s *Server) effectiveRole(p principal, project string) (model.Role, bool) {
	if p.userID == "" { // API token (no user) — global by its role
		return p.role, true
	}
	if p.role == model.RoleOwner || p.role == model.RoleAdmin {
		return p.role, true
	}
	m, ok, err := s.Store.GetProjectMember(project, p.userID)
	if err != nil || !ok {
		return "", false
	}
	return m.Role, true
}

// identify resolves the caller from a bearer JWT or API token.
func (s *Server) identify(r *http.Request) (principal, bool) {
	tok := bearerToken(r)
	// The SSE stream is opened with EventSource, which cannot set an Authorization
	// header, so /api/events accepts the token as a same-origin query parameter.
	if tok == "" && r.URL.Path == "/api/events" {
		tok = r.URL.Query().Get("access_token")
	}
	if tok == "" {
		return principal{}, false
	}
	if strings.HasPrefix(tok, "cdt_") {
		if t, ok := s.Auth.LookupAPIToken(tok); ok {
			return principal{role: t.Role}, true
		}
		return principal{}, false
	}
	claims, err := s.Auth.ParseJWT(tok)
	if err != nil {
		return principal{}, false
	}
	return principal{role: claims.Role, userID: claims.Sub, email: claims.Email}, true
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(h[len("Bearer "):])
	}
	return ""
}

func withPrincipal(r *http.Request, p principal) context.Context {
	return context.WithValue(r.Context(), principalKey, p)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "time": time.Now()})
}

// login verifies credentials and returns a JWT plus the user.
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if s.Auth == nil {
		writeErr(w, http.StatusServiceUnavailable, "auth not configured")
		return
	}
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	u, err := s.Auth.Authenticate(body.Email, body.Password)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	token, err := s.Auth.IssueJWT(u)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": u})
}

// authConfig reports which auth options the UI should present. Today only email
// (password) is supported; this lets the landing page avoid advertising methods
// that are not actually wired (e.g. no OAuth providers are configured).
func (s *Server) authConfig(w http.ResponseWriter, _ *http.Request) {
	providers := []string{}
	for _, name := range []string{"google", "github"} {
		if p, ok := s.OAuth[name]; ok && p != nil && p.ClientID != "" {
			providers = append(providers, name)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"signup_enabled": s.Auth != nil && s.AllowSignup,
		"methods":        []string{"email"},
		"providers":      providers,
	})
}

// createPersonalProject gives a freshly-registered user a workspace they own, so
// they land somewhere usable. Best-effort; a failure doesn't block sign-in.
func (s *Server) createPersonalProject(u model.User) model.Project {
	base := slugify(firstNonEmptyStr(u.Name, strings.SplitN(u.Email, "@", 2)[0]))
	proj := model.Project{ID: base + "-" + shortID(), Name: displayName(u.Name, u.Email) + "'s workspace", CreatedAt: time.Now()}
	if err := s.Store.CreateProject(proj); err == nil {
		_ = s.Store.AddProjectMember(model.ProjectMember{ProjectID: proj.ID, UserID: u.ID, Role: model.RoleOwner, CreatedAt: time.Now()})
	}
	return proj
}

// register is the public self-serve signup. It creates a member user and a
// personal project they own (so they land in a usable workspace), then logs them
// in. Disabled unless AllowSignup is set.
func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	if s.Auth == nil {
		writeErr(w, http.StatusServiceUnavailable, "auth not configured")
		return
	}
	if !s.AllowSignup {
		writeErr(w, http.StatusForbidden, "self-serve signup is disabled on this server")
		return
	}
	var body struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	body.Email = strings.ToLower(strings.TrimSpace(body.Email))
	if !strings.Contains(body.Email, "@") || strings.Contains(body.Email, " ") {
		writeErr(w, http.StatusBadRequest, "a valid email is required")
		return
	}
	if len(body.Password) < 8 {
		writeErr(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	u, err := s.Auth.CreateUser(body.Email, body.Name, body.Password, model.RoleMember)
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}

	// Give the new user a personal project they own, so they have somewhere to work.
	proj := s.createPersonalProject(u)

	token, err := s.Auth.IssueJWT(u)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"token":   token,
		"user":    u,
		"project": map[string]string{"id": proj.ID, "name": proj.Name},
	})
}

func firstNonEmptyStr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func displayName(name, email string) string {
	if n := strings.TrimSpace(name); n != "" {
		return n
	}
	return strings.SplitN(email, "@", 2)[0]
}

// shortID returns a short random hex suffix for generated ids.
func shortID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "0000"
	}
	return hex.EncodeToString(b)
}

// me returns the current authenticated user.
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	if s.Auth == nil {
		writeJSON(w, http.StatusOK, map[string]any{"auth": false})
		return
	}
	p, ok := r.Context().Value(principalKey).(principal)
	if !ok || p.userID == "" {
		// Could be a valid API token with no user; report role only.
		if ok {
			writeJSON(w, http.StatusOK, map[string]any{"role": p.role})
			return
		}
		writeErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	u, found, err := s.Auth.GetUser(p.userID)
	if err != nil || !found {
		writeErr(w, http.StatusUnauthorized, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) listTokens(w http.ResponseWriter, _ *http.Request) {
	if s.Auth == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	toks, err := s.Auth.ListAPITokens()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if toks == nil {
		toks = []model.APIToken{}
	}
	writeJSON(w, http.StatusOK, toks)
}

func (s *Server) createToken(w http.ResponseWriter, r *http.Request) {
	if s.Auth == nil {
		writeErr(w, http.StatusServiceUnavailable, "auth not configured")
		return
	}
	var body struct {
		Name string     `json:"name"`
		Role model.Role `json:"role"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body)
	if body.Role == "" {
		body.Role = model.RoleMember
	}
	if !auth.IsValidRole(body.Role) {
		writeErr(w, http.StatusBadRequest, "invalid role")
		return
	}
	plain, t, err := s.Auth.CreateAPIToken(body.Name, body.Role)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The plaintext token is returned once, here only.
	writeJSON(w, http.StatusCreated, map[string]any{"token": plain, "meta": t})
}

func (s *Server) deleteToken(w http.ResponseWriter, r *http.Request) {
	if s.Auth == nil {
		writeErr(w, http.StatusServiceUnavailable, "auth not configured")
		return
	}
	if err := s.Auth.DeleteAPIToken(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) listComponents(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"types": components.Types()})
}

// projectID resolves the selected project from the X-Conduit-Project header,
// defaulting to the built-in default project.
func projectID(r *http.Request) string {
	if p := r.Header.Get("X-Conduit-Project"); p != "" {
		return p
	}
	return model.DefaultProjectID
}

// listProjects returns the projects the caller may access: a member sees only
// projects they belong to, while API tokens and global owner/admin users see all.
// (In open mode — Auth disabled, used by tests — every project is returned.)
func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	ps, err := s.Store.ListProjects()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if ps == nil {
		ps = []model.Project{}
	}
	if s.Auth != nil {
		p, _ := r.Context().Value(principalKey).(principal)
		visible := make([]model.Project, 0, len(ps))
		for _, proj := range ps {
			if _, ok := s.effectiveRole(p, proj.ID); ok {
				visible = append(visible, proj)
			}
		}
		ps = visible
	}
	writeJSON(w, http.StatusOK, ps)
}

// builtinProjects are the seeded projects that ship with Conduit and cannot be
// deleted (they are re-created and their built-in workflows re-synced on startup).
var builtinProjects = map[string]bool{
	model.DefaultProjectID: true,
	seed.FactoryProjectID:  true,
	seed.GitHubProjectID:   true,
}

// deleteProject removes a user-created project and everything scoped to it. Only a
// project owner/admin (or a global owner/admin) may do so; built-in projects are
// refused.
func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if builtinProjects[id] {
		writeErr(w, http.StatusBadRequest, "built-in projects cannot be deleted")
		return
	}
	if _, ok, _ := s.Store.GetProject(id); !ok {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	if role, ok := s.callerRole(r, id); !ok || (role != model.RoleOwner && role != model.RoleAdmin) {
		writeErr(w, http.StatusForbidden, "only a project owner or admin can delete a project")
		return
	}
	if err := s.Store.DeleteProject(id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": id})
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeErr(w, http.StatusBadRequest, "project name is required")
		return
	}
	p := model.Project{ID: body.ID, Name: body.Name, CreatedAt: time.Now()}
	if p.ID == "" {
		p.ID = slugify(body.Name)
	}
	if existing, ok, _ := s.Store.GetProject(p.ID); ok {
		writeErr(w, http.StatusConflict, "project already exists: "+existing.ID)
		return
	}
	if err := s.Store.CreateProject(p); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The creator becomes the project owner.
	if cp, ok := r.Context().Value(principalKey).(principal); ok && cp.userID != "" {
		_ = s.Store.AddProjectMember(model.ProjectMember{ProjectID: p.ID, UserID: cp.userID, Role: model.RoleOwner, CreatedAt: time.Now()})
	}
	writeJSON(w, http.StatusCreated, p)
}

// callerRole returns the caller's effective role for a project (for in-handler checks).
func (s *Server) callerRole(r *http.Request, project string) (model.Role, bool) {
	if s.Auth == nil { // open mode (auth disabled, e.g. tests): treat as owner
		return model.RoleOwner, true
	}
	p, ok := r.Context().Value(principalKey).(principal)
	if !ok {
		return "", false
	}
	return s.effectiveRole(p, project)
}

func (s *Server) listMembers(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	members, err := s.Store.ListProjectMembers(pid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Enrich with email/name for display.
	for i := range members {
		if u, ok, _ := s.Store.GetUserByID(members[i].UserID); ok {
			members[i].Email = u.Email
			members[i].Name = u.Name
		}
	}
	writeJSON(w, http.StatusOK, members)
}

func (s *Server) addMember(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	if role, ok := s.callerRole(r, pid); !ok || (role != model.RoleOwner && role != model.RoleAdmin) {
		writeErr(w, http.StatusForbidden, "only a project owner or admin can manage members")
		return
	}
	var body struct {
		Email string     `json:"email"`
		Role  model.Role `json:"role"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Role == "" {
		body.Role = model.RoleMember
	}
	if !auth.IsValidRole(body.Role) {
		writeErr(w, http.StatusBadRequest, "invalid role")
		return
	}
	u, ok, err := s.Store.GetUserByEmail(strings.ToLower(strings.TrimSpace(body.Email)))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "no user with that email (create the user first)")
		return
	}
	m := model.ProjectMember{ProjectID: pid, UserID: u.ID, Role: body.Role, CreatedAt: time.Now()}
	if err := s.Store.AddProjectMember(m); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	m.Email, m.Name = u.Email, u.Name
	writeJSON(w, http.StatusCreated, m)
}

func (s *Server) removeMember(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	if role, ok := s.callerRole(r, pid); !ok || (role != model.RoleOwner && role != model.RoleAdmin) {
		writeErr(w, http.StatusForbidden, "only a project owner or admin can manage members")
		return
	}
	if err := s.Store.RemoveProjectMember(pid, r.PathValue("userID")); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	if !s.callerIsGlobalAdmin(r) {
		writeErr(w, http.StatusForbidden, "admin only")
		return
	}
	users, err := s.Store.ListUsers()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if users == nil {
		users = []model.User{}
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	if s.Auth == nil {
		writeErr(w, http.StatusServiceUnavailable, "auth not configured")
		return
	}
	if !s.callerIsGlobalAdmin(r) {
		writeErr(w, http.StatusForbidden, "admin only")
		return
	}
	var body struct {
		Email    string     `json:"email"`
		Name     string     `json:"name"`
		Password string     `json:"password"`
		Role     model.Role `json:"role"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Role == "" {
		body.Role = model.RoleMember
	}
	if !auth.IsValidRole(body.Role) {
		writeErr(w, http.StatusBadRequest, "invalid role")
		return
	}
	u, err := s.Auth.CreateUser(body.Email, body.Name, body.Password, body.Role)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

func (s *Server) callerIsGlobalAdmin(r *http.Request) bool {
	if s.Auth == nil {
		return true // open mode (tests)
	}
	p, ok := r.Context().Value(principalKey).(principal)
	return ok && (p.role == model.RoleOwner || p.role == model.RoleAdmin)
}

func (s *Server) listWorkflows(w http.ResponseWriter, r *http.Request) {
	wfs, err := s.Store.ListWorkflows(projectID(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if wfs == nil {
		wfs = []model.Workflow{}
	}
	writeJSON(w, http.StatusOK, wfs)
}

func (s *Server) getWorkflow(w http.ResponseWriter, r *http.Request) {
	wf, ok, err := s.Store.GetWorkflow(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok || wf.ProjectID != projectID(r) {
		writeErr(w, http.StatusNotFound, "workflow not found")
		return
	}
	writeJSON(w, http.StatusOK, wf)
}

func (s *Server) putWorkflow(w http.ResponseWriter, r *http.Request) {
	var wf model.Workflow
	if err := json.NewDecoder(r.Body).Decode(&wf); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid workflow json")
		return
	}
	wf.ID = r.PathValue("id")
	wf.ProjectID = projectID(r) // scope to the selected project
	if problems := validate.Workflow(wf); len(problems) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error":   "workflow is invalid",
			"details": problems,
		})
		return
	}
	if err := s.Store.PutWorkflow(wf); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Pick up any schedule changes immediately.
	if s.Scheduler != nil {
		_ = s.Scheduler.Sync()
	}
	writeJSON(w, http.StatusOK, wf)
}

// listSecrets returns secret names only — never values, scoped to the project.
func (s *Server) listSecrets(w http.ResponseWriter, r *http.Request) {
	if s.Secrets == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	names, err := s.Secrets.List(projectID(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if names == nil {
		names = []string{}
	}
	writeJSON(w, http.StatusOK, names)
}

func (s *Server) putSecret(w http.ResponseWriter, r *http.Request) {
	if s.Secrets == nil {
		writeErr(w, http.StatusServiceUnavailable, "secrets are not configured")
		return
	}
	var body struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.Secrets.Set(projectID(r), r.PathValue("name"), body.Value); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": r.PathValue("name")})
}

func (s *Server) deleteSecret(w http.ResponseWriter, r *http.Request) {
	if s.Secrets == nil {
		writeErr(w, http.StatusServiceUnavailable, "secrets are not configured")
		return
	}
	if err := s.Secrets.Delete(projectID(r), r.PathValue("name")); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- project config variables (${var:NAME}): plain, non-secret, per project ---

func (s *Server) listVars(w http.ResponseWriter, r *http.Request) {
	vars, err := s.Store.ListVars(projectID(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if vars == nil {
		vars = map[string]string{}
	}
	writeJSON(w, http.StatusOK, vars)
}

func (s *Server) putVar(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.Store.PutVar(projectID(r), r.PathValue("name"), body.Value); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": r.PathValue("name")})
}

func (s *Server) deleteVar(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteVar(projectID(r), r.PathValue("name")); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) listSchedules(w http.ResponseWriter, _ *http.Request) {
	if s.Scheduler == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, s.Scheduler.Schedules())
}

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.Store.ListRuns(projectID(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if runs == nil {
		runs = []model.Run{}
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) createRun(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WorkflowID string `json:"workflow_id"`
		Title      string `json:"title"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	wf, ok, err := s.Store.GetWorkflow(body.WorkflowID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok || wf.ProjectID != projectID(r) {
		writeErr(w, http.StatusNotFound, "workflow not found")
		return
	}
	run := s.Engine.Enqueue(wf, body.Title, nil)
	writeJSON(w, http.StatusCreated, run)
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	run, ok, err := s.Store.GetRun(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok || run.ProjectID != projectID(r) {
		writeErr(w, http.StatusNotFound, "run not found")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) approve(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NodeID   string `json:"node_id"`
		Decision string `json:"decision"` // approved | rejected
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Decision == "" {
		body.Decision = "approved"
	}
	if !s.Engine.Resolve(r.PathValue("id"), body.NodeID, body.Decision) {
		writeErr(w, http.StatusConflict, "no step is awaiting approval")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "decision": body.Decision})
}

// cancel stops a run in any non-terminal state. It is idempotent: canceling a run
// that already finished returns its current status rather than an error, so the UI
// always leaves the running state cleanly.
func (s *Server) cancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, ok, err := s.Store.GetRun(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok || run.ProjectID != projectID(r) {
		writeErr(w, http.StatusNotFound, "run not found")
		return
	}
	canceled := s.Engine.Cancel(id)
	// Re-read so the response reflects the final/authoritative status.
	if latest, ok, _ := s.Store.GetRun(id); ok {
		run = latest
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "canceled": canceled, "status": run.Status})
}

// rerun re-queues a finished run to execute again from a chosen node (typically a
// failed stage). Upstream successes and run memory are preserved.
func (s *Server) rerun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		NodeID string `json:"node_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.NodeID == "" {
		writeErr(w, http.StatusBadRequest, "node_id is required")
		return
	}
	run, ok, err := s.Store.GetRun(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok || run.ProjectID != projectID(r) {
		writeErr(w, http.StatusNotFound, "run not found")
		return
	}
	snap, err := s.Engine.Rerun(id, body.NodeID)
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

// hook starts a run from an inbound webhook, binding the posted JSON body as the
// run's input (exposed by the trigger node to downstream steps).
func (s *Server) hook(w http.ResponseWriter, r *http.Request) {
	wf, ok, err := s.Store.GetWorkflow(r.PathValue("workflowID"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "workflow not found")
		return
	}
	var input map[string]any
	if r.Body != nil {
		// Tolerate an empty or non-JSON body; input is optional.
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&input)
	}
	run := s.Engine.Enqueue(wf, "webhook: "+wf.Name, input)
	writeJSON(w, http.StatusCreated, run)
}

// events streams run events as SSE. An optional ?run=<id> filters to one run.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	runFilter := r.URL.Query().Get("run")
	id, ch := s.Store.Broker().Subscribe()
	defer s.Store.Broker().Unsubscribe(id)

	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if runFilter != "" && ev.RunID != runFilter {
				continue
			}
			b, _ := json.Marshal(ev)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, b)
			flusher.Flush()
		}
	}
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": msg})
}

// slugify turns a project name into a URL/id-safe slug.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "project"
	}
	return out
}

// listAudit returns recent audit entries (global admin only).
func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	if !s.callerIsGlobalAdmin(r) {
		writeErr(w, http.StatusForbidden, "admin only")
		return
	}
	entries, err := s.Store.ListAudit(200)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if entries == nil {
		entries = []model.AuditEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}
