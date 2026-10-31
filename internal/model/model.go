// Package model holds the core domain types for Conduit: workflows (a DAG of
// components) and the runs produced when a workflow executes.
package model

import "time"

// Status is the lifecycle state of a run or an individual step.
type Status string

const (
	StatusPending   Status = "pending"
	StatusTriggered Status = "triggered"
	StatusRunning   Status = "running"
	StatusWaiting   Status = "waiting"
	StatusSuccess   Status = "success"
	StatusCreated   Status = "created"
	StatusFailed    Status = "failed"
	StatusSkipped   Status = "skipped"
	StatusCanceled  Status = "canceled"
)

// Kind distinguishes the three roles a node can play on the canvas.
const (
	KindTrigger = "trigger"
	KindStep    = "step"
	KindStatus  = "status"
)

// Field is one ordered configuration row shown on a node. Order and icon are
// preserved so the canvas renders exactly as authored.
type Field struct {
	Key   string `json:"key"`
	Icon  string `json:"icon"`
	Value string `json:"value"`
}

// Node is a single component on the canvas.
type Node struct {
	ID     string  `json:"id"`
	Type   string  `json:"type"` // github, timegate, http, ssh, if, wait, approval, memory, status
	Title  string  `json:"title"`
	Icon   string  `json:"icon"`
	Kind   string  `json:"kind"`
	X      int     `json:"x"`
	Y      int     `json:"y"`
	Config []Field `json:"config"`
}

// Field returns the configured value for a key, or "" if absent.
func (n Node) Field(key string) string {
	for _, f := range n.Config {
		if f.Key == key {
			return f.Value
		}
	}
	return ""
}

// Edge connects two nodes. Label gates which successor runs after an outcome
// ("success", "failed", "approved", or "" for unconditional).
type Edge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label,omitempty"`
}

// Workflow is a named DAG of nodes and edges.
type Workflow struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// StepRun is the execution record for one node within a run.
type StepRun struct {
	NodeID    string         `json:"node_id"`
	Status    Status         `json:"status"`
	StartedAt *time.Time     `json:"started_at,omitempty"`
	EndedAt   *time.Time     `json:"ended_at,omitempty"`
	Output    map[string]any `json:"output,omitempty"`
	Logs      []string       `json:"logs,omitempty"`
}

// Run is one execution of a workflow.
type Run struct {
	ID         string              `json:"id"`
	WorkflowID string              `json:"workflow_id"`
	Title      string              `json:"title"`
	Status     Status              `json:"status"`
	CreatedAt  time.Time           `json:"created_at"`
	FinishedAt *time.Time          `json:"finished_at,omitempty"`
	Steps      map[string]*StepRun `json:"steps"`
	Memory     map[string]any      `json:"memory"`
	// Input is the payload a run was started with (e.g. a webhook body); the
	// trigger node exposes it to downstream steps.
	Input map[string]any `json:"input,omitempty"`
}

// Event is a message published to live subscribers (the SSE stream).
type Event struct {
	Type    string    `json:"type"` // run.started, step.updated, log, run.waiting, run.finished
	RunID   string    `json:"run_id"`
	NodeID  string    `json:"node_id,omitempty"`
	Status  Status    `json:"status,omitempty"`
	Level   string    `json:"level,omitempty"` // info, run, ok, fail
	Message string    `json:"message,omitempty"`
	Step    *StepRun  `json:"step,omitempty"`
	Run     *Run      `json:"run,omitempty"`
	Time    time.Time `json:"time"`
}
