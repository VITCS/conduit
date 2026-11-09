package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"conduit/internal/components"
	"conduit/internal/model"
)

func newTaskID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return "task_" + hex.EncodeToString(b[:])
}

// taskWorkflow resolves the automation a task runs on: its own WorkflowID, else
// the project's factory workflow, else the first workflow in the project.
func (s *Server) taskWorkflow(t model.Task) (model.Workflow, bool) {
	if t.WorkflowID != "" {
		if wf, ok, _ := s.Store.GetWorkflow(t.WorkflowID); ok {
			return wf, true
		}
	}
	wfs, _ := s.Store.ListWorkflows(t.ProjectID)
	for _, wf := range wfs {
		if wf.ID == "wf_factory" {
			return wf, true
		}
	}
	if len(wfs) > 0 {
		return wfs[0], true
	}
	return model.Workflow{}, false
}

// GET /api/tasks?stage=
func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	stage := model.TaskStage(r.URL.Query().Get("stage"))
	tasks, err := s.Store.ListTasks(projectID(r), stage)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tasks == nil {
		tasks = []model.Task{}
	}
	writeJSON(w, http.StatusOK, tasks)
}

// POST /api/tasks — manual intake into Backlog.
func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title       string `json:"title"`
		Summary     string `json:"summary"`
		Description string `json:"description"`
		Source      string `json:"source"`
		SourceRef   string `json:"source_ref"`
		SourceURL   string `json:"source_url"`
		WorkflowID  string `json:"workflow_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Title == "" {
		writeErr(w, http.StatusBadRequest, "title is required")
		return
	}
	now := time.Now()
	t := model.Task{
		ID: newTaskID(), ProjectID: projectID(r), WorkflowID: body.WorkflowID,
		Title: body.Title, Summary: body.Summary, Description: body.Description,
		Source: pickStr(body.Source, "manual"), SourceRef: body.SourceRef, SourceURL: body.SourceURL,
		Stage: model.StageBacklog, CreatedAt: now, UpdatedAt: now, StageChangedAt: now,
		History: []model.StageEvent{{Stage: model.StageBacklog, At: now}},
	}
	created, err := s.Store.CreateTask(t)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Store.Broker().Publish(model.Event{Type: "task.created", Task: &created, Time: now})
	writeJSON(w, http.StatusCreated, created)
}

// GET /api/tasks/{id} — task + its run history.
func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	t, ok, err := s.Store.GetTask(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok || t.ProjectID != projectID(r) {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	runs, _ := s.Store.ListRunsByTask(t.ID)
	if runs == nil {
		runs = []model.Run{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"task": t, "runs": runs})
}

// POST /api/tasks/{id}/start — the authorization boundary: enqueue the factory.
func (s *Server) startTask(w http.ResponseWriter, r *http.Request) {
	t, ok, err := s.Store.GetTask(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok || t.ProjectID != projectID(r) {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	wf, ok := s.taskWorkflow(t)
	if !ok {
		writeErr(w, http.StatusBadRequest, "no workflow to run for this task's project")
		return
	}
	run := s.Engine.EnqueueForTask(wf, t)
	now := time.Now()
	snap, _, _ := s.Store.MutateTask(t.ID, func(tt *model.Task) {
		tt.CurrentRunID = run.ID
		tt.WorkflowID = wf.ID
		tt.SetStage(model.StageImplement, now)
	})
	s.Store.Broker().Publish(model.Event{Type: "task.updated", RunID: run.ID, Task: &snap, Time: now})
	writeJSON(w, http.StatusCreated, map[string]any{"task": snap, "run": run})
}

// POST /api/tasks/{id}/stage — manual stage override / archive.
func (s *Server) setTaskStage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Stage string `json:"stage"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	stage := model.TaskStage(body.Stage)
	if !model.ValidStage(stage) {
		writeErr(w, http.StatusBadRequest, "invalid stage")
		return
	}
	t, ok, _ := s.Store.GetTask(r.PathValue("id"))
	if !ok || t.ProjectID != projectID(r) {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	now := time.Now()
	snap, _, err := s.Store.MutateTask(t.ID, func(tt *model.Task) {
		tt.SetStage(stage, now)
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Store.Broker().Publish(model.Event{Type: "task.updated", Task: &snap, Time: now})
	writeJSON(w, http.StatusOK, snap)
}

// POST /api/tasks/{id}/notes — add a comment to a task.
func (s *Server) addTaskNote(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text string `json:"text"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if strings.TrimSpace(body.Text) == "" {
		writeErr(w, http.StatusBadRequest, "text is required")
		return
	}
	t, ok, _ := s.Store.GetTask(r.PathValue("id"))
	if !ok || t.ProjectID != projectID(r) {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	author := ""
	if p, ok := r.Context().Value(principalKey).(principal); ok {
		author = p.email
	}
	now := time.Now()
	snap, _, err := s.Store.MutateTask(t.ID, func(tt *model.Task) {
		tt.Notes = append(tt.Notes, model.TaskNote{Author: author, Text: body.Text, At: now})
		tt.UpdatedAt = now
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Store.Broker().Publish(model.Event{Type: "task.updated", Task: &snap, Time: now})
	writeJSON(w, http.StatusCreated, snap)
}

// DELETE /api/tasks/{id}
func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	t, ok, _ := s.Store.GetTask(r.PathValue("id"))
	if !ok || t.ProjectID != projectID(r) {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	if err := s.Store.DeleteTask(t.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/intake/{project}?source=github|jira|notion — webhook intake. Accepts
// a raw provider payload (or the normalized fields) and creates a Backlog task,
// scoring intake confidence. If the project var `auto_start_confidence` is set and
// the score clears it, the task is started automatically.
func (s *Server) intake(w http.ResponseWriter, r *http.Request) {
	var raw map[string]any
	if r.Body != nil {
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&raw)
	}
	project := r.PathValue("project")
	src := r.URL.Query().Get("source")
	nt := normalizeIntake(src, raw)
	if nt.Title == "" {
		writeErr(w, http.StatusBadRequest, "could not derive a task title from the payload")
		return
	}

	score, reason := components.HeuristicConfidence(nt.Summary, nt.Description)
	now := time.Now()
	t := model.Task{
		ID: newTaskID(), ProjectID: project,
		Title: nt.Title, Summary: nt.Summary, Description: nt.Description,
		Source: pickStr(nt.Source, "intake"), SourceRef: nt.SourceRef, SourceURL: nt.SourceURL,
		Confidence: score,
		Stage:      model.StageBacklog, CreatedAt: now, UpdatedAt: now, StageChangedAt: now,
		History: []model.StageEvent{{Stage: model.StageBacklog, At: now}},
	}
	created, err := s.Store.CreateTask(t)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Store.Broker().Publish(model.Event{Type: "task.created", Task: &created, Time: now})

	// Auto-start policy: start when confidence clears the project threshold.
	autoStarted := false
	if thr := autoStartThreshold(s, project); thr > 0 && score >= thr {
		if wf, ok := s.taskWorkflow(created); ok {
			run := s.Engine.EnqueueForTask(wf, created)
			snap, _, _ := s.Store.MutateTask(created.ID, func(tt *model.Task) {
				tt.CurrentRunID = run.ID
				tt.WorkflowID = wf.ID
				tt.SetStage(model.StageImplement, time.Now())
			})
			s.Store.Broker().Publish(model.Event{Type: "task.updated", RunID: run.ID, Task: &snap, Time: time.Now()})
			created = snap
			autoStarted = true
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"task": created, "confidence": score, "confidence_reason": reason, "auto_started": autoStarted,
	})
}

// autoStartThreshold reads the project's auto_start_confidence var (0 = disabled).
func autoStartThreshold(s *Server, project string) int {
	v, ok, _ := s.Store.GetVar(project, "auto_start_confidence")
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

type normalizedIntake struct{ Title, Summary, Description, Source, SourceRef, SourceURL string }

// normalizeIntake maps a provider webhook payload (GitHub issues, Jira, or a
// normalized/Notion body) to task fields.
func normalizeIntake(source string, raw map[string]any) normalizedIntake {
	switch strings.ToLower(source) {
	case "github":
		if iss, ok := raw["issue"].(map[string]any); ok {
			ref := ""
			if n, ok := iss["number"].(float64); ok {
				ref = fmt.Sprintf("#%d", int(n))
			}
			if repo, ok := raw["repository"].(map[string]any); ok {
				if fn, ok := repo["full_name"].(string); ok {
					ref = fn + ref
				}
			}
			return normalizedIntake{
				Title: str(iss["title"]), Summary: str(iss["title"]), Description: str(iss["body"]),
				Source: "github", SourceRef: ref, SourceURL: str(iss["html_url"]),
			}
		}
	case "jira":
		if iss, ok := raw["issue"].(map[string]any); ok {
			key := str(iss["key"])
			summary, desc := "", ""
			if f, ok := iss["fields"].(map[string]any); ok {
				summary = str(f["summary"])
				desc = str(f["description"]) // plain string; ADF objects are skipped
			}
			return normalizedIntake{
				Title: pickStr(summary, key), Summary: summary, Description: desc,
				Source: "jira", SourceRef: key, SourceURL: str(raw["url"]),
			}
		}
	}
	// Normalized body (manual, Notion automations, or a pre-shaped payload).
	return normalizedIntake{
		Title:       pickStr(str(raw["title"]), str(raw["source_ref"]), str(raw["summary"])),
		Summary:     str(raw["summary"]),
		Description: str(raw["description"]),
		Source:      pickStr(str(raw["source"]), source),
		SourceRef:   str(raw["source_ref"]),
		SourceURL:   str(raw["source_url"]),
	}
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func pickStr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
