package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

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
		tt.Stage = model.StageImplement
		tt.StageChangedAt = now
		tt.UpdatedAt = now
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
		tt.Stage = stage
		tt.StageChangedAt = now
		tt.UpdatedAt = now
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Store.Broker().Publish(model.Event{Type: "task.updated", Task: &snap, Time: now})
	writeJSON(w, http.StatusOK, snap)
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

// POST /api/intake/{project} — webhook intake: create a Backlog task from an issue.
func (s *Server) intake(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title       string `json:"title"`
		Summary     string `json:"summary"`
		Description string `json:"description"`
		Source      string `json:"source"`
		SourceRef   string `json:"source_ref"`
		SourceURL   string `json:"source_url"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	title := pickStr(body.Title, body.SourceRef, body.Summary)
	if title == "" {
		writeErr(w, http.StatusBadRequest, "title, source_ref or summary is required")
		return
	}
	now := time.Now()
	t := model.Task{
		ID: newTaskID(), ProjectID: r.PathValue("project"),
		Title: title, Summary: body.Summary, Description: body.Description,
		Source: pickStr(body.Source, "intake"), SourceRef: body.SourceRef, SourceURL: body.SourceURL,
		Stage: model.StageBacklog, CreatedAt: now, UpdatedAt: now, StageChangedAt: now,
	}
	created, err := s.Store.CreateTask(t)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Store.Broker().Publish(model.Event{Type: "task.created", Task: &created, Time: now})
	writeJSON(w, http.StatusCreated, created)
}

func pickStr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
