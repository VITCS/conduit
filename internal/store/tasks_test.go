package store

import (
	"testing"
	"time"

	"conduit/internal/model"
)

func TestMemTaskCRUD(t *testing.T) {
	s := NewMem()
	now := time.Now()
	_, _ = s.CreateTask(model.Task{ID: "task_1", ProjectID: "p", Title: "A", Stage: model.StageBacklog, CreatedAt: now})
	_, _ = s.CreateTask(model.Task{ID: "task_2", ProjectID: "p", Title: "B", Stage: model.StageImplement, CreatedAt: now.Add(time.Second)})
	_, _ = s.CreateTask(model.Task{ID: "task_3", ProjectID: "other", Title: "C", Stage: model.StageBacklog, CreatedAt: now})

	all, _ := s.ListTasks("p", "")
	if len(all) != 2 {
		t.Fatalf("want 2 tasks in p, got %d", len(all))
	}
	if all[0].ID != "task_2" {
		t.Errorf("want newest first (task_2), got %s", all[0].ID)
	}
	backlog, _ := s.ListTasks("p", model.StageBacklog)
	if len(backlog) != 1 || backlog[0].ID != "task_1" {
		t.Errorf("stage filter failed: %+v", backlog)
	}

	snap, ok, _ := s.MutateTask("task_1", func(tk *model.Task) { tk.Stage = model.StageVerify })
	if !ok || snap.Stage != model.StageVerify {
		t.Fatalf("mutate failed: ok=%v stage=%s", ok, snap.Stage)
	}

	if err := s.DeleteTask("task_1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok, _ := s.GetTask("task_1"); ok {
		t.Error("task_1 should be gone")
	}
}

func TestMemListRunsByTask(t *testing.T) {
	s := NewMem()
	_, _ = s.CreateRun(&model.Run{ID: "run_1", ProjectID: "p", TaskID: "task_1", CreatedAt: time.Now()})
	_, _ = s.CreateRun(&model.Run{ID: "run_2", ProjectID: "p", TaskID: "task_1", CreatedAt: time.Now().Add(time.Second)})
	_, _ = s.CreateRun(&model.Run{ID: "run_3", ProjectID: "p", TaskID: "task_2", CreatedAt: time.Now()})

	runs, _ := s.ListRunsByTask("task_1")
	if len(runs) != 2 {
		t.Fatalf("want 2 runs for task_1, got %d", len(runs))
	}
	if runs[0].ID != "run_2" {
		t.Errorf("want newest first (run_2), got %s", runs[0].ID)
	}
}
