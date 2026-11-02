// Package store persists workflows and runs and carries a small publish/subscribe
// broker for live run events.
//
// Store is the interface the engine and API depend on. Two implementations ship:
// Mem (in-memory, used for tests and quick starts) and Pg (Postgres, for durable
// state). Snapshots returned to callers are deep copies, so the execution
// goroutine can keep mutating a run while the API reads it.
package store

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"conduit/internal/model"
)

// Store is the persistence contract.
type Store interface {
	// Broker returns the live-event broker shared by this store.
	Broker() *Broker

	// Projects are tenancy boundaries for workflows and runs.
	CreateProject(p model.Project) error
	GetProject(id string) (model.Project, bool, error)
	ListProjects() ([]model.Project, error)

	PutWorkflow(w model.Workflow) error
	GetWorkflow(id string) (model.Workflow, bool, error)
	// ListWorkflows returns workflows in a project, or all when projectID is "".
	ListWorkflows(projectID string) ([]model.Workflow, error)

	// CreateRun stores a new run and returns a snapshot.
	CreateRun(r *model.Run) (model.Run, error)
	GetRun(id string) (model.Run, bool, error)
	// ListRuns returns runs in a project (newest first), or all when projectID is "".
	ListRuns(projectID string) ([]model.Run, error)

	// ClaimRun atomically leases one claimable run to a worker: a queued run, or a
	// running/waiting run whose lease has expired (crash recovery). ok is false if
	// none is available. The claimed run's status is set to running.
	ClaimRun(workerID string, lease time.Duration) (model.Run, bool, error)
	// RenewLease extends a run's lease while a worker holds it.
	RenewLease(runID, workerID string, lease time.Duration) error

	// MutateRun applies fn to the stored run atomically and returns a snapshot
	// of the result. ok is false if the run does not exist.
	MutateRun(id string, fn func(*model.Run)) (snap model.Run, ok bool, err error)

	// Project membership (per-project roles).
	AddProjectMember(m model.ProjectMember) error
	GetProjectMember(projectID, userID string) (model.ProjectMember, bool, error)
	ListProjectMembers(projectID string) ([]model.ProjectMember, error)
	RemoveProjectMember(projectID, userID string) error

	// Secrets are stored per project as opaque ciphertext; encryption happens
	// above the store (internal/secrets), so the store never sees plaintext.
	PutSecret(projectID, name string, ciphertext []byte) error
	GetSecret(projectID, name string) (ciphertext []byte, ok bool, err error)
	ListSecretNames(projectID string) ([]string, error)
	DeleteSecret(projectID, name string) error

	// Project config variables: non-secret, plain key/value set once per project
	// (e.g. owner/repo/model) and referenced in node config as ${var:NAME}.
	PutVar(projectID, name, value string) error
	GetVar(projectID, name string) (string, bool, error)
	ListVars(projectID string) (map[string]string, error)
	DeleteVar(projectID, name string) error

	// Users & API tokens (authentication).
	CreateUser(u model.User) error
	GetUserByEmail(email string) (model.User, bool, error)
	GetUserByID(id string) (model.User, bool, error)
	ListUsers() ([]model.User, error)
	CountUsers() (int, error)

	// External identities (OAuth: google / github).
	GetUserByIdentity(provider, subject string) (model.User, bool, error)
	AddUserIdentity(id model.UserIdentity) error

	CreateAPIToken(t model.APIToken) error
	GetAPITokenByHash(hash string) (model.APIToken, bool, error)
	ListAPITokens() ([]model.APIToken, error)
	DeleteAPIToken(id string) error

	// Audit log of mutating actions.
	AppendAudit(e model.AuditEntry) error
	ListAudit(limit int) ([]model.AuditEntry, error)
}

// New returns the default (in-memory) store.
func New() *Mem { return NewMem() }

// Mem is an in-memory, concurrency-safe Store.
type Mem struct {
	mu         sync.RWMutex
	projects   map[string]model.Project
	members    map[string]model.ProjectMember // key: projectID\x00userID
	workflows  map[string]model.Workflow
	runs       map[string]*model.Run
	order      []string                      // run IDs, oldest first
	claimedBy  map[string]string             // runID -> workerID
	leaseExp   map[string]time.Time          // runID -> lease expiry
	secrets    map[string][]byte             // key: projectID\x00name
	vars       map[string]string             // key: projectID\x00name
	users      map[string]model.User         // by ID
	identities map[string]model.UserIdentity // key: provider\x00subject
	tokens     map[string]model.APIToken     // by ID
	audit      []model.AuditEntry
	broker     *Broker
}

// NewMem builds an empty in-memory store.
func NewMem() *Mem {
	return &Mem{
		projects:   map[string]model.Project{},
		members:    map[string]model.ProjectMember{},
		workflows:  map[string]model.Workflow{},
		runs:       map[string]*model.Run{},
		claimedBy:  map[string]string{},
		leaseExp:   map[string]time.Time{},
		secrets:    map[string][]byte{},
		vars:       map[string]string{},
		users:      map[string]model.User{},
		identities: map[string]model.UserIdentity{},
		tokens:     map[string]model.APIToken{},
		broker:     newBroker(),
	}
}

func (s *Mem) ClaimRun(workerID string, lease time.Duration) (model.Run, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	// Oldest-first scan for a claimable run.
	for _, id := range s.order {
		r, ok := s.runs[id]
		if !ok {
			continue
		}
		exp, leased := s.leaseExp[id]
		claimable := r.Status == model.StatusQueued ||
			((r.Status == model.StatusRunning || r.Status == model.StatusWaiting) && leased && exp.Before(now))
		if !claimable {
			continue
		}
		r.Status = model.StatusRunning
		s.claimedBy[id] = workerID
		s.leaseExp[id] = now.Add(lease)
		return cloneRun(r), true, nil
	}
	return model.Run{}, false, nil
}

func (s *Mem) RenewLease(runID, workerID string, lease time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claimedBy[runID] == workerID {
		s.leaseExp[runID] = time.Now().Add(lease)
	}
	return nil
}

func mkey(a, b string) string { return a + "\x00" + b }

func (s *Mem) AddProjectMember(m model.ProjectMember) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.members[mkey(m.ProjectID, m.UserID)] = m
	return nil
}

func (s *Mem) GetProjectMember(projectID, userID string) (model.ProjectMember, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.members[mkey(projectID, userID)]
	return m, ok, nil
}

func (s *Mem) ListProjectMembers(projectID string) ([]model.ProjectMember, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []model.ProjectMember{}
	for _, m := range s.members {
		if m.ProjectID == projectID {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *Mem) RemoveProjectMember(projectID, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.members, mkey(projectID, userID))
	return nil
}

func (s *Mem) CreateProject(p model.Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.projects[p.ID] = p
	return nil
}

func (s *Mem) GetProject(id string) (model.Project, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.projects[id]
	return p, ok, nil
}

func (s *Mem) ListProjects() ([]model.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Project, 0, len(s.projects))
	for _, p := range s.projects {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *Mem) CreateUser(u model.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[u.ID] = u
	return nil
}

func (s *Mem) GetUserByEmail(email string) (model.User, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.users {
		if u.Email == email {
			return u, true, nil
		}
	}
	return model.User{}, false, nil
}

func (s *Mem) GetUserByID(id string) (model.User, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[id]
	return u, ok, nil
}

func (s *Mem) GetUserByIdentity(provider, subject string) (model.User, bool, error) {
	s.mu.RLock()
	id, ok := s.identities[provider+"\x00"+subject]
	if !ok {
		s.mu.RUnlock()
		return model.User{}, false, nil
	}
	u, uok := s.users[id.UserID]
	s.mu.RUnlock()
	return u, uok, nil
}

func (s *Mem) AddUserIdentity(id model.UserIdentity) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.identities[id.Provider+"\x00"+id.Subject] = id
	return nil
}

func (s *Mem) ListUsers() ([]model.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.User, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	return out, nil
}

func (s *Mem) CountUsers() (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.users), nil
}

func (s *Mem) CreateAPIToken(t model.APIToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[t.ID] = t
	return nil
}

func (s *Mem) GetAPITokenByHash(hash string) (model.APIToken, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.tokens {
		if t.Hash == hash {
			return t, true, nil
		}
	}
	return model.APIToken{}, false, nil
}

func (s *Mem) ListAPITokens() ([]model.APIToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.APIToken, 0, len(s.tokens))
	for _, t := range s.tokens {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *Mem) DeleteAPIToken(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, id)
	return nil
}

func (s *Mem) AppendAudit(e model.AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audit = append(s.audit, e)
	// Bound in-memory growth.
	if len(s.audit) > 5000 {
		s.audit = s.audit[len(s.audit)-5000:]
	}
	return nil
}

func (s *Mem) ListAudit(limit int) ([]model.AuditEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit > len(s.audit) {
		limit = len(s.audit)
	}
	out := make([]model.AuditEntry, 0, limit)
	for i := len(s.audit) - 1; i >= 0 && len(out) < limit; i-- { // newest first
		out = append(out, s.audit[i])
	}
	return out, nil
}

func (s *Mem) PutSecret(projectID, name string, ciphertext []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]byte, len(ciphertext))
	copy(cp, ciphertext)
	s.secrets[mkey(projectID, name)] = cp
	return nil
}

func (s *Mem) GetSecret(projectID, name string) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.secrets[mkey(projectID, name)]
	if !ok {
		return nil, false, nil
	}
	cp := make([]byte, len(b))
	copy(cp, b)
	return cp, true, nil
}

func (s *Mem) ListSecretNames(projectID string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	prefix := projectID + "\x00"
	out := []string{}
	for k := range s.secrets {
		if strings.HasPrefix(k, prefix) {
			out = append(out, strings.TrimPrefix(k, prefix))
		}
	}
	sort.Strings(out)
	return out, nil
}

func (s *Mem) DeleteSecret(projectID, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.secrets, mkey(projectID, name))
	return nil
}

func (s *Mem) PutVar(projectID, name, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vars[mkey(projectID, name)] = value
	return nil
}

func (s *Mem) GetVar(projectID, name string) (string, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.vars[mkey(projectID, name)]
	return v, ok, nil
}

func (s *Mem) ListVars(projectID string) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]string{}
	prefix := projectID + "\x00"
	for k, v := range s.vars {
		if strings.HasPrefix(k, prefix) {
			out[strings.TrimPrefix(k, prefix)] = v
		}
	}
	return out, nil
}

func (s *Mem) DeleteVar(projectID, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.vars, mkey(projectID, name))
	return nil
}

func (s *Mem) Broker() *Broker { return s.broker }

func (s *Mem) PutWorkflow(w model.Workflow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workflows[w.ID] = w
	return nil
}

func (s *Mem) GetWorkflow(id string) (model.Workflow, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	w, ok := s.workflows[id]
	return w, ok, nil
}

func (s *Mem) ListWorkflows(projectID string) ([]model.Workflow, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Workflow, 0, len(s.workflows))
	for _, w := range s.workflows {
		if projectID == "" || w.ProjectID == projectID {
			out = append(out, w)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *Mem) CreateRun(r *model.Run) (model.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[r.ID] = r
	s.order = append(s.order, r.ID)
	return cloneRun(r), nil
}

func (s *Mem) GetRun(id string) (model.Run, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.runs[id]
	if !ok {
		return model.Run{}, false, nil
	}
	return cloneRun(r), true, nil
}

func (s *Mem) ListRuns(projectID string) ([]model.Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Run, 0, len(s.order))
	for i := len(s.order) - 1; i >= 0; i-- { // newest first
		if r, ok := s.runs[s.order[i]]; ok {
			if projectID == "" || r.ProjectID == projectID {
				out = append(out, cloneRun(r))
			}
		}
	}
	return out, nil
}

func (s *Mem) MutateRun(id string, fn func(*model.Run)) (model.Run, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	if !ok {
		return model.Run{}, false, nil
	}
	fn(r)
	return cloneRun(r), true, nil
}

// cloneRun deep-copies a run so callers never share mutable state.
func cloneRun(r *model.Run) model.Run {
	b, _ := json.Marshal(r)
	var c model.Run
	_ = json.Unmarshal(b, &c)
	return c
}

// Compile-time assurance that Mem satisfies Store.
var _ Store = (*Mem)(nil)
