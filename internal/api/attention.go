package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"conduit/internal/engine"
	"conduit/internal/model"
)

// AttentionItem is one thing waiting on a person ("Needs you").
type AttentionItem struct {
	// Kind: question (an agent asked something), approval (a gate is waiting),
	// stuck (a task is blocked), review (a pull request waits for review).
	Kind        string   `json:"kind"`
	ProjectID   string   `json:"project_id"`
	ProjectName string   `json:"project_name"`
	RunID       string   `json:"run_id,omitempty"`
	NodeID      string   `json:"node_id,omitempty"`
	TaskID      string   `json:"task_id,omitempty"`
	ReviewID    string   `json:"review_id,omitempty"`
	Title       string   `json:"title"`          // run, task or PR title
	Step        string   `json:"step,omitempty"` // the step that is waiting
	Text        string   `json:"text,omitempty"` // the question, the gate's summary, or why it is stuck
	Options     []string `json:"options,omitempty"`
	// Reason, for a stuck item: stalled (a running step made no progress),
	// not_started (a queued run was never picked up), or blocked (a blocked task).
	Reason string    `json:"reason,omitempty"`
	URL    string    `json:"url,omitempty"`
	Since  time.Time `json:"since"`
}

// Attention is the "Needs you" list for the caller, across their projects.
type Attention struct {
	Items     []AttentionItem `json:"items"`
	ByProject map[string]int  `json:"by_project"`
	Total     int             `json:"total"`
}

// GET /api/attention — everything waiting on the caller in every project they can
// see: agent questions, approval gates, blocked tasks and pending PR reviews.
func (s *Server) attention(w http.ResponseWriter, r *http.Request) {
	ps, err := s.Store.ListProjects()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The default project exists implicitly (requests without a project use it).
	hasDefault := false
	for _, proj := range ps {
		hasDefault = hasDefault || proj.ID == model.DefaultProjectID
	}
	if !hasDefault {
		ps = append(ps, model.Project{ID: model.DefaultProjectID, Name: "Default"})
	}
	p, _ := r.Context().Value(principalKey).(principal)
	out := Attention{Items: []AttentionItem{}, ByProject: map[string]int{}}
	for _, proj := range ps {
		if s.Auth != nil {
			if _, ok := s.effectiveRole(p, proj.ID); !ok {
				continue
			}
		}
		items := s.projectAttention(proj)
		out.Items = append(out.Items, items...)
		if len(items) > 0 {
			out.ByProject[proj.ID] = len(items)
		}
	}
	// Questions first (an agent is blocked on them), then approvals, then the rest;
	// oldest first within a kind.
	rank := map[string]int{"question": 0, "approval": 1, "stuck": 2, "review": 3}
	sort.SliceStable(out.Items, func(i, j int) bool {
		a, b := out.Items[i], out.Items[j]
		if rank[a.Kind] != rank[b.Kind] {
			return rank[a.Kind] < rank[b.Kind]
		}
		return a.Since.Before(b.Since)
	})
	out.Total = len(out.Items)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) projectAttention(proj model.Project) []AttentionItem {
	var items []AttentionItem
	runs, _ := s.Store.ListRuns(proj.ID)
	wfs := map[string]model.Workflow{}
	stepTitle := func(wfID, nodeID string) string {
		wf, ok := wfs[wfID]
		if !ok {
			wf, _, _ = s.Store.GetWorkflow(wfID)
			wfs[wfID] = wf
		}
		for _, n := range wf.Nodes {
			if n.ID == nodeID {
				return n.Title
			}
		}
		return nodeID
	}
	stuckAfter := 10 * time.Minute
	if s.Engine != nil && s.Engine.StallStuck > 0 {
		stuckAfter = s.Engine.StallStuck
	}
	now := time.Now()
	for _, run := range runs {
		// A queued run nobody picked up (no worker free, or none running).
		if run.Status == model.StatusQueued && now.Sub(run.CreatedAt) >= stuckAfter {
			items = append(items, AttentionItem{Kind: "stuck", Reason: "not_started", ProjectID: proj.ID, ProjectName: proj.Name,
				RunID: run.ID, TaskID: run.TaskID, Title: run.Title, Since: run.CreatedAt,
				Text: fmt.Sprintf("Queued %s ago and not started. Check that the server's workers are running.", ago(now.Sub(run.CreatedAt)))})
			continue
		}
		// A running step the watchdog flagged as stuck (no progress for a while).
		if run.Status == model.StatusRunning {
			for id, st := range run.Steps {
				if st == nil || st.Status != model.StatusRunning || st.Output["stall"] != "stuck" {
					continue
				}
				since := run.CreatedAt
				if v, _ := st.Output["stall_since"].(string); v != "" {
					if t, err := time.Parse(time.RFC3339, v); err == nil {
						since = t
					}
				}
				items = append(items, AttentionItem{Kind: "stuck", Reason: "stalled", ProjectID: proj.ID, ProjectName: proj.Name,
					RunID: run.ID, NodeID: id, TaskID: run.TaskID, Title: run.Title, Step: stepTitle(run.WorkflowID, id), Since: since,
					Text: fmt.Sprintf("Still running, but no progress for %s. Open the run to look at its log, or cancel it.", ago(now.Sub(since)))})
			}
			continue
		}
		if run.Status != model.StatusWaiting {
			continue
		}
		for id, st := range run.Steps {
			if st == nil || st.Status != model.StatusWaiting {
				continue
			}
			it := AttentionItem{Kind: "approval", ProjectID: proj.ID, ProjectName: proj.Name,
				RunID: run.ID, NodeID: id, TaskID: run.TaskID, Title: run.Title,
				Step: stepTitle(run.WorkflowID, id), Since: run.CreatedAt}
			if st.StartedAt != nil {
				it.Since = *st.StartedAt
			}
			if k, _ := st.Output["kind"].(string); k == "question" {
				it.Kind = "question"
				it.Text, _ = st.Output["question"].(string)
				if opts, ok := st.Output["options"].([]any); ok {
					for _, o := range opts {
						if s, ok := o.(string); ok {
							it.Options = append(it.Options, s)
						}
					}
				} else if opts, ok := st.Output["options"].([]string); ok {
					it.Options = opts
				}
			} else {
				it.Text, _ = st.Output["summary"].(string)
				if len(it.Text) > 400 {
					it.Text = it.Text[:400] + "…"
				}
			}
			items = append(items, it)
		}
	}
	tasks, _ := s.Store.ListTasks(proj.ID, "")
	for _, t := range tasks {
		if t.Stage != model.StageBlocked {
			continue
		}
		it := AttentionItem{Kind: "stuck", Reason: "blocked", ProjectID: proj.ID, ProjectName: proj.Name,
			TaskID: t.ID, RunID: t.CurrentRunID, Title: t.Title, Since: t.StageChangedAt}
		if n := len(t.Notes); n > 0 {
			it.Text = t.Notes[n-1].Text
		}
		items = append(items, it)
	}
	reqs, _ := s.Store.ListReviewRequests(proj.ID, "pending")
	for _, rq := range reqs {
		title := strings.TrimSpace(rq.Title)
		if title == "" {
			title = rq.Owner + "/" + rq.Repo
		}
		items = append(items, AttentionItem{Kind: "review", ProjectID: proj.ID, ProjectName: proj.Name,
			ReviewID: rq.ID, Title: title, Text: rq.Owner + "/" + rq.Repo + " #" + strconv.Itoa(rq.Number),
			URL: rq.URL, Since: rq.CreatedAt})
	}
	return items
}

// POST /api/runs/{id}/answer — answer a step's question: {node_id, answer}.
func (s *Server) answer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NodeID string `json:"node_id"`
		Answer string `json:"answer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	body.Answer = strings.TrimSpace(body.Answer)
	if body.Answer == "" {
		writeErr(w, http.StatusBadRequest, "write an answer, or pick one of the suggestions")
		return
	}
	if len(body.Answer) > 4000 {
		writeErr(w, http.StatusBadRequest, "answer is too long (4000 characters max)")
		return
	}
	run, ok, err := s.Store.GetRun(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok || run.ProjectID != projectID(r) {
		writeErr(w, http.StatusNotFound, "run not found")
		return
	}
	st := run.Steps[body.NodeID]
	if st == nil || st.Status != model.StatusWaiting || st.Output["kind"] != "question" {
		writeErr(w, http.StatusConflict, "this step is not waiting for an answer")
		return
	}
	if !s.Engine.ResolveWith(run.ID, body.NodeID, engine.Decision{Decision: engine.DecisionAnswered, Feedback: body.Answer}) {
		writeErr(w, http.StatusConflict, "this step is not waiting for an answer")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ago formats a duration for people: "under a minute", "12 min", "3 h 5 min".
func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "under a minute"
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	default:
		return fmt.Sprintf("%d h %d min", int(d.Hours()), int(d.Minutes())%60)
	}
}
