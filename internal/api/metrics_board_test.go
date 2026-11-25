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
		{ID: "r1", Loops: map[string]int{"a\x00b": 2}, // recovered via retry
			Usage: &model.Usage{Calls: 3, CostUSD: 0.3, ByModel: map[string]*model.Usage{
				"claude-haiku-5-5":  {Model: "claude-haiku-5-5", Calls: 2, InputTokens: 100, CostUSD: 0.1},
				"claude-sonnet-5-5": {Model: "claude-sonnet-5-5", Calls: 1, OutputTokens: 50, CostUSD: 0.2},
			}}},
		{ID: "r2"},
		{ID: "r3", Usage: &model.Usage{Calls: 1, SubscriptionUSD: 0.5, ByModel: map[string]*model.Usage{
			"claude-sonnet-5-5": {Model: "claude-sonnet-5-5", Calls: 1, SubscriptionUSD: 0.5}}}},
	}
	m := computeBoardMetrics(tasks, runs, base.Add(60*time.Minute))

	u := m.Usage
	if u.RunsWithUsage != 2 || u.Calls != 4 || u.CostUSD != 0.3 || u.SubscriptionUSD != 0.5 || u.AvgRunCostUSD != 0.4 {
		t.Errorf("usage = %+v", u)
	}
	if len(u.ByModel) != 2 || u.ByModel[0].Model != "claude-sonnet-5-5" || u.ByModel[0].Calls != 2 {
		t.Errorf("by model = %+v", u.ByModel)
	}
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
	if m.Runs != 3 || m.RetryRuns != 1 || m.RetryRate != 1.0/3 {
		t.Errorf("retry: runs=%d retry=%d rate=%v", m.Runs, m.RetryRuns, m.RetryRate)
	}
	if m.InterventionPct != 0.5 { // 1 blocked / 2 started
		t.Errorf("intervention = %v", m.InterventionPct)
	}
}
