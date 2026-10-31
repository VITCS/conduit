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

// Project is a tenancy boundary: workflows and runs belong to exactly one.
type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// DefaultProjectID is the project seeded on first run.
const DefaultProjectID = "default"

// Workflow is a named DAG of nodes and edges, scoped to a project.
type Workflow struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
	Nodes     []Node `json:"nodes"`
	Edges     []Edge `json:"edges"`
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
	ProjectID  string              `json:"project_id"`
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

// Role is a user's or token's permission level.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
	RoleViewer Role = "viewer"
)

// CanWrite reports whether a role may perform mutating operations.
func (r Role) CanWrite() bool { return r != RoleViewer && r != "" }

// User is an account that can sign in.
type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	Role         Role      `json:"role"`
	PasswordHash string    `json:"-"` // never serialized
	CreatedAt    time.Time `json:"created_at"`
}

// APIToken is a long-lived credential for programmatic access (CI, webhooks).
// Only the SHA-256 hash is stored; the plaintext is shown once at creation.
type APIToken struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Hash       string     `json:"-"`
	Role       Role       `json:"role"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
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
