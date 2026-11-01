// Package worker runs queued workflow runs. Each worker claims a run from the
// store with a lease (so exactly one worker across all instances executes it),
// executes it via the engine while renewing the lease, and moves on. A crashed
// worker's lease expires and another worker reclaims the run.
package worker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"time"

	"conduit/internal/engine"
	"conduit/internal/store"
)

// Pool is a set of workers draining the run queue.
type Pool struct {
	store  store.Store
	engine *engine.Engine
	id     string

	N            int           // workers in this instance
	PollInterval time.Duration // idle poll interval
	Lease        time.Duration // claim lease duration
}

// New builds a worker pool with sensible defaults.
func New(st store.Store, eng *engine.Engine, n int) *Pool {
	if n <= 0 {
		n = 4
	}
	return &Pool{
		store: st, engine: eng, id: workerID(),
		N: n, PollInterval: 250 * time.Millisecond, Lease: 30 * time.Second,
	}
}

func workerID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "worker-" + hex.EncodeToString(b)
}

// Start launches the workers until ctx is canceled.
func (p *Pool) Start(ctx context.Context) {
	slog.Info("worker pool starting", "workers", p.N, "id", p.id, "lease_ms", p.Lease.Milliseconds())
	for i := 0; i < p.N; i++ {
		go p.loop(ctx)
	}
}

func (p *Pool) loop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		run, ok, err := p.store.ClaimRun(p.id, p.Lease)
		if err != nil {
			slog.Error("claim run", "err", err)
		}
		if !ok {
			select {
			case <-ctx.Done():
				return
			case <-time.After(p.PollInterval):
			}
			continue
		}
		p.execute(ctx, run.ID)
	}
}

// execute runs one claimed run, renewing its lease until execution returns.
func (p *Pool) execute(ctx context.Context, runID string) {
	stop := make(chan struct{})
	defer close(stop)
	go p.renew(ctx, runID, stop)

	run, ok, err := p.store.GetRun(runID)
	if err != nil || !ok {
		return
	}
	p.engine.ExecuteClaimed(ctx, run)
}

// renew extends the lease at half the lease interval so long runs (and runs
// waiting on approval) are not reclaimed by another worker.
func (p *Pool) renew(ctx context.Context, runID string, stop <-chan struct{}) {
	t := time.NewTicker(p.Lease / 2)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-t.C:
			_ = p.store.RenewLease(runID, p.id, p.Lease)
		}
	}
}
