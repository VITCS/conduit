// Package api wires HTTP routes to the store and engine. It speaks JSON for the
// REST surface and Server-Sent Events for the live run stream.
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"conduit/internal/components"
	"conduit/internal/engine"
	"conduit/internal/model"
	"conduit/internal/scheduler"
	"conduit/internal/store"
	"conduit/internal/validate"
)

// Server bundles the dependencies the handlers need.
type Server struct {
	Store     store.Store
	Engine    *engine.Engine
	Scheduler *scheduler.Scheduler // optional; nil disables schedule endpoints/sync
	Web       fs.FS                // static assets (the UI)
}

// Routes returns the HTTP handler for the whole app.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/components", s.listComponents)
	mux.HandleFunc("GET /api/workflows", s.listWorkflows)
	mux.HandleFunc("GET /api/workflows/{id}", s.getWorkflow)
	mux.HandleFunc("PUT /api/workflows/{id}", s.putWorkflow)
	mux.HandleFunc("GET /api/schedules", s.listSchedules)
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

	return logging(mux)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "time": time.Now()})
}

func (s *Server) listComponents(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"types": components.Types()})
}

func (s *Server) listWorkflows(w http.ResponseWriter, _ *http.Request) {
	wfs, err := s.Store.ListWorkflows()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wfs)
}

func (s *Server) getWorkflow(w http.ResponseWriter, r *http.Request) {
	wf, ok, err := s.Store.GetWorkflow(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
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

func (s *Server) listSchedules(w http.ResponseWriter, _ *http.Request) {
	if s.Scheduler == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, s.Scheduler.Schedules())
}

func (s *Server) listRuns(w http.ResponseWriter, _ *http.Request) {
	runs, err := s.Store.ListRuns()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
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
	if !ok {
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
	if !ok {
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

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if r.URL.Path != "/api/events" {
			fmt.Printf("%s %s %s\n", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
		}
	})
}
