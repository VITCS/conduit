package model

import "time"

// TaskStage is a column on the factory board. A task's stage is a projection of
// its run's progress — the engine advances it as the run crosses stage-boundary
// nodes (Node.Stage), so the board and the automation never drift.
type TaskStage string

const (
	StageBacklog   TaskStage = "backlog"
	StageImplement TaskStage = "implement"
	StageVerify    TaskStage = "verify"
	StageDone      TaskStage = "done"
	StageBlocked   TaskStage = "blocked"
)

// ValidStage reports whether s is a known stage.
func ValidStage(s TaskStage) bool {
	switch s {
	case StageBacklog, StageImplement, StageVerify, StageDone, StageBlocked:
		return true
	}
	return false
}

// StageEvent records a task entering a stage — the raw material for time-in-stage
// and lead-time metrics.
type StageEvent struct {
	Stage TaskStage `json:"stage"`
	At    time.Time `json:"at"`
}

// TaskNote is a human comment on a task.
type TaskNote struct {
	Author string    `json:"author,omitempty"`
	Text   string    `json:"text"`
	At     time.Time `json:"at"`
}

// Task is the durable unit of work on the board. It spans runs: one execution of
// the factory workflow for this task is a Run (Run.TaskID), and the task carries
// the source issue, the current stage, confidence and the produced artifacts.
type Task struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	WorkflowID  string    `json:"workflow_id"`
	Title       string    `json:"title"`
	Source      string    `json:"source,omitempty"` // jira | notion | github | manual
	SourceRef   string    `json:"source_ref,omitempty"`
	SourceURL   string    `json:"source_url,omitempty"`
	Summary     string    `json:"summary,omitempty"`
	Description string    `json:"description,omitempty"`
	Stage       TaskStage `json:"stage"`
	Confidence  int       `json:"confidence,omitempty"` // 0–100, from triage

	CurrentRunID string `json:"current_run_id,omitempty"`
	Branch       string `json:"branch,omitempty"`
	PRURL        string `json:"pr_url,omitempty"`

	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	StageChangedAt time.Time `json:"stage_changed_at"`

	History []StageEvent `json:"history,omitempty"`
	Notes   []TaskNote   `json:"notes,omitempty"`
}

// SetStage updates the task's stage and appends a transition to its history (only
// when the stage actually changes), keeping timestamps consistent. Use it for
// every stage change so the metrics have an accurate record.
func (t *Task) SetStage(s TaskStage, at time.Time) {
	t.UpdatedAt = at
	if t.Stage == s && len(t.History) > 0 {
		return
	}
	t.Stage = s
	t.StageChangedAt = at
	t.History = append(t.History, StageEvent{Stage: s, At: at})
}
