// Package model holds the core domain types for Conduit: workflows (a DAG of
// components) and the runs produced when a workflow executes.
package model

import "time"

// Status is the lifecycle state of a run or an individual step.
type Status string

const (
	StatusPending   Status = "pending"
	StatusQueued    Status = "queued"
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
	// Stage, when set, is the board stage this node belongs to. As a task's run
	// enters the node, the engine projects this onto the task (see model.TaskStage).
	Stage string `json:"stage,omitempty"`
	// Disabled bypasses this step during a run: the executor is not called, the step
	// is marked skipped, and the run continues down the node's success edges. Used to
	// make an optional step (e.g. a CI check a token can't read) opt-in.
	Disabled bool `json:"disabled,omitempty"`
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
//
// MaxAttempts marks a bounded retry back-edge: when set (> 0) on a "failed" edge,
// a failing source re-enters the loop body at To up to MaxAttempts times (carrying
// the failure context forward) before the failure is routed terminally. It is the
// only kind of back-edge the validator permits.
type Edge struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Label       string `json:"label,omitempty"`
	MaxAttempts int    `json:"max_attempts,omitempty"`
}

// Project is a tenancy boundary: workflows and runs belong to exactly one.
type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// DefaultProjectID is the project seeded on first run.
const DefaultProjectID = "default"

// ProjectMember grants a user a role within a project. A per-project role
// overrides the user's global role for that project's resources.
type ProjectMember struct {
	ProjectID string    `json:"project_id"`
	UserID    string    `json:"user_id"`
	Email     string    `json:"email,omitempty"` // filled on read for display
	Name      string    `json:"name,omitempty"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// Workflow is a named DAG of nodes and edges, scoped to a project.
type Workflow struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
	Nodes     []Node `json:"nodes"`
	Edges     []Edge `json:"edges"`
	// Rev and SeedHash track built-in (seeded) workflows so the server can upgrade
	// them to a newer shipped definition on startup without ever overwriting a copy
	// the user has edited. Both are zero/empty for user-created workflows and for
	// built-ins seeded before this mechanism existed. See seed.SyncBuiltins.
	Rev      int    `json:"rev,omitempty"`
	SeedHash string `json:"seed_hash,omitempty"`
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
	// Loops tracks how many times each retry back-edge (keyed "from\x00to") has
	// fired, so a bounded recovery loop survives restarts.
	Loops map[string]int `json:"loops,omitempty"`
	// Input is the payload a run was started with (e.g. a webhook body); the
	// trigger node exposes it to downstream steps.
	Input map[string]any `json:"input,omitempty"`
	// TaskID links this run to a board task, when it was started for one.
	TaskID string `json:"task_id,omitempty"`
	// ResumeFrom, when set, tells the engine to begin execution from these node IDs
	// instead of the workflow's triggers — used to re-run a failed stage without
	// redoing the successful upstream work. It is consumed (cleared) at execution.
	ResumeFrom []string `json:"resume_from,omitempty"`
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

// UserIdentity links an external identity provider (google / github) to a user.
// It is the persistence layer that lets a person sign in with Google or GitHub and
// resolve to the same Conduit user across sessions. (provider, subject) is unique.
type UserIdentity struct {
	Provider  string    `json:"provider"` // "google" | "github"
	Subject   string    `json:"subject"`  // the provider's stable user id
	UserID    string    `json:"user_id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
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

// AuditEntry records a mutating action for the audit log.
type AuditEntry struct {
	ID      string    `json:"id"`
	Time    time.Time `json:"time"`
	Actor   string    `json:"actor"`   // user email or "token"/"anonymous"
	Method  string    `json:"method"`  // HTTP method
	Path    string    `json:"path"`    // request path
	Project string    `json:"project"` // selected project, if any
	Status  int       `json:"status"`  // response status code
}

// Event is a message published to live subscribers (the SSE stream).
type Event struct {
	Type    string `json:"type"` // run.started, step.updated, log, run.waiting, run.finished
	RunID   string `json:"run_id"`
	NodeID  string `json:"node_id,omitempty"`
	Status  Status `json:"status,omitempty"`
	Level   string `json:"level,omitempty"` // info, run, ok, fail
	Message string `json:"message,omitempty"`
	// Detail carries longer human-readable context (e.g. the agent's suggestion
	// shown at an approval gate), rendered from the node's templated summary.
	Detail string   `json:"detail,omitempty"`
	Step   *StepRun `json:"step,omitempty"`
	Run    *Run     `json:"run,omitempty"`
	// Task carries board-task changes on "task.created" / "task.updated" events.
	Task *Task     `json:"task,omitempty"`
	Time time.Time `json:"time"`
}
