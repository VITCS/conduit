// Package api wires HTTP routes to the store and engine. It speaks JSON for the
// REST surface and Server-Sent Events for the live run stream.
package api

import (
	"context"
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
	"conduit/internal/model"
	"conduit/internal/scheduler"
	"conduit/internal/secrets"
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
	Web       fs.FS                // static assets (the UI)
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

	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("GET /api/auth/me", s.me)
	mux.HandleFunc("GET /api/tokens", s.listTokens)
	mux.HandleFunc("POST /api/tokens", s.createToken)
	mux.HandleFunc("DELETE /api/tokens/{id}", s.deleteToken)
	mux.HandleFunc("GET /api/components", s.listComponents)
	mux.HandleFunc("GET /api/projects", s.listProjects)
	mux.HandleFunc("POST /api/projects", s.createProject)
	mux.HandleFunc("GET /api/workflows", s.listWorkflows)
	mux.HandleFunc("GET /api/workflows/{id}", s.getWorkflow)
	mux.HandleFunc("PUT /api/workflows/{id}", s.putWorkflow)
	mux.HandleFunc("GET /api/schedules", s.listSchedules)
	mux.HandleFunc("GET /api/secrets", s.listSecrets)
	mux.HandleFunc("PUT /api/secrets/{name}", s.putSecret)
	mux.HandleFunc("DELETE /api/secrets/{name}", s.deleteSecret)
	mux.HandleFunc("GET /api/runs", s.listRuns)
	mux.HandleFunc("POST /api/runs", s.createRun)
	mux.HandleFunc("GET /api/runs/{id}", s.getRun)
	mux.HandleFunc("POST /api/runs/{id}/approve", s.approve)
	mux.HandleFunc("POST /api/runs/{id}/cancel", s.cancel)
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

	return logging(s.authMiddleware(mux))
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
		if path == "/api/health" || path == "/api/auth/login" {
			next.ServeHTTP(w, r)
			return
		}

		// Webhooks authenticate with an API token (bearer or ?token=).
		if strings.HasPrefix(path, "/api/hooks/") {
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
		// Mutations require write permission.
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !p.role.CanWrite() {
			writeErr(w, http.StatusForbidden, "your role is read-only")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey, p)))
	})
}

// identify resolves the caller from a bearer JWT or API token.
func (s *Server) identify(r *http.Request) (principal, bool) {
	tok := bearerToken(r)
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

func (s *Server) listProjects(w http.ResponseWriter, _ *http.Request) {
	ps, err := s.Store.ListProjects()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if ps == nil {
		ps = []model.Project{}
	}
	writeJSON(w, http.StatusOK, ps)
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
	writeJSON(w, http.StatusCreated, p)
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

// listSecrets returns secret names only — never values.
func (s *Server) listSecrets(w http.ResponseWriter, _ *http.Request) {
	if s.Secrets == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	names, err := s.Secrets.List()
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
	if err := s.Secrets.Set(r.PathValue("name"), body.Value); err != nil {
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
	if err := s.Secrets.Delete(r.PathValue("name")); err != nil {
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
	run := s.Engine.Start(wf, body.Title)
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

// cancel stops a running or waiting run.
func (s *Server) cancel(w http.ResponseWriter, r *http.Request) {
	if !s.Engine.Cancel(r.PathValue("id")) {
		writeErr(w, http.StatusConflict, "run is not active")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
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
	run := s.Engine.StartWithInput(wf, "webhook: "+wf.Name, input)
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

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if r.URL.Path != "/api/events" {
			fmt.Printf("%s %s %s\n", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
		}
	})
}
