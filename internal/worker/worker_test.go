package worker

import (
	"context"
	"testing"
	"time"

	"conduit/internal/engine"
	"conduit/internal/model"
	"conduit/internal/store"
)

func smallWorkflow() model.Workflow {
	return model.Workflow{
		ID: "wf_w", ProjectID: "default", Name: "W",
		Nodes: []model.Node{
			{ID: "t", Type: "github", Kind: model.KindTrigger, Title: "Trigger"},
			{ID: "s", Type: "status", Kind: model.KindStatus, Title: "Done"},
		},
		Edges: []model.Edge{{From: "t", To: "s"}},
	}
}

func TestWorkerExecutesQueuedRun(t *testing.T) {
	st := store.NewMem()
	wf := smallWorkflow()
	if err := st.PutWorkflow(wf); err != nil {
		t.Fatal(err)
	}
	eng := engine.New(st)
	eng.StepDelay, eng.TriggerDelay = 0, 0

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := New(st, eng, 2)
	p.PollInterval = 10 * time.Millisecond
	p.Start(ctx)

	// Enqueue a run; a worker should claim and complete it.
	run := eng.Enqueue(wf, "", nil)
	if run.Status != model.StatusQueued {
		t.Fatalf("enqueued run status = %s, want queued", run.Status)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r, ok, _ := st.GetRun(run.ID)
		if ok && r.Status == model.StatusSuccess {
			if r.Steps["s"].Status != model.StatusCreated {
				t.Errorf("status node = %s, want created", r.Steps["s"].Status)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("worker did not complete the queued run within 3s")
}

func TestExpiredLeaseIsReclaimed(t *testing.T) {
	st := store.NewMem()
	_ = st.PutWorkflow(smallWorkflow())
	// A run claimed by a now-dead worker with a tiny lease.
	_, _ = st.CreateRun(&model.Run{ID: "stuck", WorkflowID: "wf_w", ProjectID: "default",
		Status: model.StatusQueued, CreatedAt: time.Now(), Steps: map[string]*model.StepRun{}, Memory: map[string]any{}})
	if _, ok, _ := st.ClaimRun("dead", 10*time.Millisecond); !ok {
		t.Fatal("initial claim failed")
	}
	// After the lease expires, another worker can reclaim it.
	time.Sleep(30 * time.Millisecond)
	if _, ok, _ := st.ClaimRun("alive", time.Minute); !ok {
		t.Error("expired lease was not reclaimable")
	}
}
