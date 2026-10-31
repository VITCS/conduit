// Package scheduler starts workflow runs on a cron schedule. A workflow opts in
// with a `schedule` trigger node carrying a `cron` config field (standard 5-field,
// optional seconds, or descriptors like "@every 1m"). The scheduler re-syncs from
// the store periodically so edits via the API take effect without a restart.
//
// Single-instance only for now: multi-instance scheduling (leader election /
// distributed locks) is a later phase.
package scheduler

import (
	"context"
	"log"
	"sync"
	"time"

	"conduit/internal/engine"
	"conduit/internal/model"
	"conduit/internal/store"

	"github.com/robfig/cron/v3"
)

// Info describes one active schedule (for the API/UI).
type Info struct {
	WorkflowID   string `json:"workflow_id"`
	WorkflowName string `json:"workflow_name"`
	NodeID       string `json:"node_id"`
	Cron         string `json:"cron"`
}

// Scheduler owns a cron engine and keeps it in sync with stored workflows.
type Scheduler struct {
	store  store.Store
	engine *engine.Engine

	mu    sync.Mutex
	cron  *cron.Cron
	ids   []cron.EntryID
	infos []Info
}

// parser accepts 5-field crons, an optional leading seconds field, and
// descriptors (@hourly, @every 30s, …).
var parser = cron.NewParser(
	cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

// New builds a scheduler bound to a store and engine.
func New(s store.Store, e *engine.Engine) *Scheduler {
	return &Scheduler{
		store:  s,
		engine: e,
		cron:   cron.New(cron.WithParser(parser)),
	}
}

// Start begins firing schedules and re-syncs every 30s until ctx is canceled.
func (s *Scheduler) Start(ctx context.Context) {
	if err := s.Sync(); err != nil {
		log.Printf("scheduler: initial sync: %v", err)
	}
	s.cron.Start()
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				s.cron.Stop()
				return
			case <-t.C:
				if err := s.Sync(); err != nil {
					log.Printf("scheduler: sync: %v", err)
				}
			}
		}
	}()
}

// Sync rebuilds cron entries from the current set of workflows.
func (s *Scheduler) Sync() error {
	wfs, err := s.store.ListWorkflows("") // all projects
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, id := range s.ids {
		s.cron.Remove(id)
	}
	s.ids = s.ids[:0]
	s.infos = s.infos[:0]

	for _, wf := range wfs {
		for _, n := range wf.Nodes {
			if n.Kind != model.KindTrigger || n.Type != "schedule" {
				continue
			}
			spec := n.Field("cron")
			if spec == "" {
				continue
			}
			wf := wf // capture per workflow
			id, err := s.cron.AddFunc(spec, func() {
				s.engine.Start(wf, "schedule: "+wf.Name)
			})
			if err != nil {
				log.Printf("scheduler: bad cron %q on %s/%s: %v", spec, wf.ID, n.ID, err)
				continue
			}
			s.ids = append(s.ids, id)
			s.infos = append(s.infos, Info{WorkflowID: wf.ID, WorkflowName: wf.Name, NodeID: n.ID, Cron: spec})
		}
	}
	return nil
}

// Schedules returns a snapshot of the active schedules.
func (s *Scheduler) Schedules() []Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Info, len(s.infos))
	copy(out, s.infos)
	return out
}
