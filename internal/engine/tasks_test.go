package engine

import (
	"testing"
	"time"

	"conduit/internal/model"
	"conduit/internal/store"
)

func TestTaskStageProjection(t *testing.T) {
	st := store.NewMem()
	e := New(st)
	now := time.Now()
	_, _ = st.CreateTask(model.Task{ID: "task_1", ProjectID: "p", Title: "A", Stage: model.StageBacklog, CreatedAt: now})
	_, _ = st.CreateRun(&model.Run{ID: "run_1", ProjectID: "p", TaskID: "task_1",
		Steps: map[string]*model.StepRun{}, Memory: map[string]any{}, CreatedAt: now})

	// Entering a node with Stage advances the task's stage.
	e.projectTaskStage("run_1", model.Node{ID: "v", Type: "github_checks", Stage: "verify"})
	if tk, _, _ := st.GetTask("task_1"); tk.Stage != model.StageVerify {
		t.Fatalf("want verify, got %s", tk.Stage)
	}

	// Terminal success → Done, with artifacts copied from run memory.
	run, _, _ := st.MutateRun("run_1", func(r *model.Run) {
		r.Memory["last_pr_url"] = "https://github.com/o/r/pull/9"
		r.Memory["work_branch"] = "conduit/ENG-1"
		r.Memory["confidence_score"] = 82
	})
	e.finalizeTask(&run, model.StatusSuccess)
	tk, _, _ := st.GetTask("task_1")
	if tk.Stage != model.StageDone {
		t.Errorf("want done, got %s", tk.Stage)
	}
	if tk.PRURL == "" || tk.Branch == "" || tk.Confidence != 82 {
		t.Errorf("artifacts not copied: %+v", tk)
	}
}

func TestTaskBlockedOnFailure(t *testing.T) {
	st := store.NewMem()
	e := New(st)
	_, _ = st.CreateTask(model.Task{ID: "task_2", ProjectID: "p", Title: "B", Stage: model.StageImplement})
	run := model.Run{ID: "run_2", ProjectID: "p", TaskID: "task_2", Memory: map[string]any{}}
	e.finalizeTask(&run, model.StatusFailed)
	if tk, _, _ := st.GetTask("task_2"); tk.Stage != model.StageBlocked {
		t.Errorf("want blocked on failure, got %s", tk.Stage)
	}
}

func TestProjectionIgnoresUnboundRun(t *testing.T) {
	st := store.NewMem()
	e := New(st)
	_, _ = st.CreateRun(&model.Run{ID: "run_3", ProjectID: "p", Memory: map[string]any{}}) // no TaskID
	// Should not panic or error when the run has no task.
	e.projectTaskStage("run_3", model.Node{ID: "v", Stage: "verify"})
}
