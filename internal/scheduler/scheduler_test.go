package scheduler

import (
	"testing"
	"time"

	"conduit/internal/engine"
	"conduit/internal/model"
	"conduit/internal/store"
)

func scheduledWorkflow(spec string) model.Workflow {
	return model.Workflow{
		ID: "wf_sched", Name: "Scheduled",
		Nodes: []model.Node{
			{ID: "t", Type: "schedule", Kind: model.KindTrigger, Title: "On schedule",
				Config: []model.Field{{Key: "cron", Value: spec}}},
			{ID: "s", Type: "status", Kind: model.KindStatus, Title: "Done"},
		},
		Edges: []model.Edge{{From: "t", To: "s"}},
	}
}

func TestScheduleFiresRun(t *testing.T) {
	st := store.NewMem()
	if err := st.PutWorkflow(scheduledWorkflow("@every 1s")); err != nil {
		t.Fatal(err)
	}
	e := engine.New(st)
	e.StepDelay, e.TriggerDelay = 0, 0

	s := New(st, e)
	if err := s.Sync(); err != nil {
		t.Fatal(err)
	}
	if len(s.Schedules()) != 1 {
		t.Fatalf("expected 1 schedule, got %d", len(s.Schedules()))
	}
	s.cron.Start()
	defer s.cron.Stop()

	// Within a few seconds the schedule should have created at least one run.
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		runs, _ := st.ListRuns()
		if len(runs) >= 1 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("schedule did not create a run within 4s")
}

func TestSyncIgnoresNonScheduleAndBadCron(t *testing.T) {
	st := store.NewMem()
	// No schedule node → no entries.
	_ = st.PutWorkflow(model.Workflow{ID: "w1", Name: "plain",
		Nodes: []model.Node{{ID: "t", Type: "github", Kind: model.KindTrigger}}})
	// A schedule node with an invalid cron → skipped, not fatal.
	_ = st.PutWorkflow(scheduledWorkflow("not a cron"))

	s := New(st, engine.New(st))
	if err := s.Sync(); err != nil {
		t.Fatalf("Sync returned error: %v", err)
	}
	if n := len(s.Schedules()); n != 0 {
		t.Errorf("expected 0 valid schedules, got %d", n)
	}
}
