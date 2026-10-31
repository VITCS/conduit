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
	"sync"

	"conduit/internal/model"
)

// Store is the persistence contract.
type Store interface {
	// Broker returns the live-event broker shared by this store.
	Broker() *Broker

	PutWorkflow(w model.Workflow) error
	GetWorkflow(id string) (model.Workflow, bool, error)
	ListWorkflows() ([]model.Workflow, error)

	// CreateRun stores a new run and returns a snapshot.
	CreateRun(r *model.Run) (model.Run, error)
	GetRun(id string) (model.Run, bool, error)
	ListRuns() ([]model.Run, error)

	// MutateRun applies fn to the stored run atomically and returns a snapshot
	// of the result. ok is false if the run does not exist.
	MutateRun(id string, fn func(*model.Run)) (snap model.Run, ok bool, err error)

	// Secrets are stored as opaque ciphertext; encryption happens above the
	// store (internal/secrets), so the store never sees plaintext.
	PutSecret(name string, ciphertext []byte) error
	GetSecret(name string) (ciphertext []byte, ok bool, err error)
	ListSecretNames() ([]string, error)
	DeleteSecret(name string) error
}

// New returns the default (in-memory) store.
func New() *Mem { return NewMem() }

// Mem is an in-memory, concurrency-safe Store.
type Mem struct {
	mu        sync.RWMutex
	workflows map[string]model.Workflow
	runs      map[string]*model.Run
	order     []string // run IDs, oldest first
	secrets   map[string][]byte
	broker    *Broker
}

// NewMem builds an empty in-memory store.
func NewMem() *Mem {
	return &Mem{
		workflows: map[string]model.Workflow{},
		runs:      map[string]*model.Run{},
		secrets:   map[string][]byte{},
		broker:    newBroker(),
	}
}

func (s *Mem) PutSecret(name string, ciphertext []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]byte, len(ciphertext))
	copy(cp, ciphertext)
	s.secrets[name] = cp
	return nil
}

func (s *Mem) GetSecret(name string) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.secrets[name]
	if !ok {
		return nil, false, nil
	}
	cp := make([]byte, len(b))
	copy(cp, b)
	return cp, true, nil
}

func (s *Mem) ListSecretNames() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.secrets))
	for k := range s.secrets {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

func (s *Mem) DeleteSecret(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.secrets, name)
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

func (s *Mem) ListWorkflows() ([]model.Workflow, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Workflow, 0, len(s.workflows))
	for _, w := range s.workflows {
		out = append(out, w)
	}
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

func (s *Mem) ListRuns() ([]model.Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Run, 0, len(s.order))
	for i := len(s.order) - 1; i >= 0; i-- { // newest first
		if r, ok := s.runs[s.order[i]]; ok {
			out = append(out, cloneRun(r))
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
