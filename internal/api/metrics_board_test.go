package api

import (
	"testing"
	"time"

	"conduit/internal/model"
)

func TestComputeBoardMetrics(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ev := func(s model.TaskStage, mins int) model.StageEvent {
		return model.StageEvent{Stage: s, At: base.Add(time.Duration(mins) * time.Minute)}
	}
	tasks := []model.Task{
		{ // done after backlog(0)→implement(10)→verify(30)→done(40)
			ID: "1", Stage: model.StageDone, CreatedAt: base,
			History: []model.StageEvent{ev("backlog", 0), ev("implement", 10), ev("verify", 30), ev("done", 40)},
		},
		{ // blocked
			ID: "2", Stage: model.StageBlocked, CreatedAt: base,
			History: []model.StageEvent{ev("backlog", 0), ev("implement", 5), ev("blocked", 15)},
		},
		{ // still in backlog (never started)
			ID: "3", Stage: model.StageBacklog, CreatedAt: base,
			History: []model.StageEvent{ev("backlog", 0)},
		},
	}
	runs := []model.Run{
		{ID: "r1", Loops: map[string]int{"a\x00b": 2}}, // recovered via retry
		{ID: "r2"},
	}
	m := computeBoardMetrics(tasks, runs, base.Add(60*time.Minute))

	if m.Tasks != 3 || m.Done != 1 || m.Blocked != 1 || m.Started != 2 {
		t.Fatalf("counts: %+v", m)
	}
	if m.ByStage[model.StageBacklog] != 1 {
		t.Errorf("backlog count = %d", m.ByStage[model.StageBacklog])
	}
	// task 1 implement dwell = 20m; verify dwell = 10m.
	if m.StageAvgMs[model.StageImplement] == 0 || m.StageAvgMs[model.StageVerify] != (10*time.Minute).Milliseconds() {
		t.Errorf("stage dwell: %+v", m.StageAvgMs)
	}
	// lead time for the one done task = 40m.
	if m.AvgLeadMs != (40 * time.Minute).Milliseconds() {
		t.Errorf("avg lead = %d", m.AvgLeadMs)
	}
	if m.Runs != 2 || m.RetryRuns != 1 || m.RetryRate != 0.5 {
		t.Errorf("retry: runs=%d retry=%d rate=%v", m.Runs, m.RetryRuns, m.RetryRate)
	}
	if m.InterventionPct != 0.5 { // 1 blocked / 2 started
		t.Errorf("intervention = %v", m.InterventionPct)
	}
}
