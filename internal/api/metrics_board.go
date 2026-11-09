package api

import (
	"net/http"
	"time"

	"conduit/internal/model"
)

// BoardMetrics is the factory-performance summary ("Done is operating data").
type BoardMetrics struct {
	Tasks   int                     `json:"tasks"`
	ByStage map[model.TaskStage]int `json:"by_stage"`
	Done    int                     `json:"done"`
	Blocked int                     `json:"blocked"`
	Started int                     `json:"started"` // tasks that left Backlog

	AvgLeadMs   int64                     `json:"avg_lead_ms"`  // created → done
	StageAvgMs  map[model.TaskStage]int64 `json:"stage_avg_ms"` // avg dwell per stage
	StageInflow map[model.TaskStage]int   `json:"stage_inflow"` // times a stage was entered

	Runs            int     `json:"runs"`
	RetryRuns       int     `json:"retry_runs"`       // runs that recovered a failed check
	RetryRate       float64 `json:"retry_rate"`       // retry_runs / runs
	InterventionPct float64 `json:"intervention_pct"` // blocked / started
}

// GET /api/metrics — factory performance for the current project.
func (s *Server) boardMetrics(w http.ResponseWriter, r *http.Request) {
	pid := projectID(r)
	tasks, err := s.Store.ListTasks(pid, "")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	runs, _ := s.Store.ListRuns(pid)
	writeJSON(w, http.StatusOK, computeBoardMetrics(tasks, runs, time.Now()))
}

func computeBoardMetrics(tasks []model.Task, runs []model.Run, now time.Time) BoardMetrics {
	m := BoardMetrics{
		ByStage:     map[model.TaskStage]int{},
		StageAvgMs:  map[model.TaskStage]int64{},
		StageInflow: map[model.TaskStage]int{},
	}
	m.Tasks = len(tasks)

	stageSum := map[model.TaskStage]int64{}
	stageN := map[model.TaskStage]int{}
	var leadSum int64
	var leadN int

	for _, t := range tasks {
		m.ByStage[t.Stage]++
		if t.Stage == model.StageDone {
			m.Done++
		}
		if t.Stage == model.StageBlocked {
			m.Blocked++
		}
		started := false
		// Dwell per stage from the transition history. The final (current) stage is
		// still open, so it counts up to now.
		for i, ev := range t.History {
			if ev.Stage != model.StageBacklog {
				started = true
			}
			m.StageInflow[ev.Stage]++
			var end time.Time
			if i+1 < len(t.History) {
				end = t.History[i+1].At
			} else {
				end = now
			}
			d := end.Sub(ev.At).Milliseconds()
			if d < 0 {
				d = 0
			}
			stageSum[ev.Stage] += d
			stageN[ev.Stage]++
		}
		if started || t.Stage != model.StageBacklog {
			m.Started++
		}
		if t.Stage == model.StageDone {
			leadSum += doneTime(t).Sub(t.CreatedAt).Milliseconds()
			leadN++
		}
	}
	for st, sum := range stageSum {
		if stageN[st] > 0 {
			m.StageAvgMs[st] = sum / int64(stageN[st])
		}
	}
	if leadN > 0 {
		m.AvgLeadMs = leadSum / int64(leadN)
	}

	m.Runs = len(runs)
	for _, r := range runs {
		if len(r.Loops) > 0 {
			m.RetryRuns++
		}
	}
	if m.Runs > 0 {
		m.RetryRate = float64(m.RetryRuns) / float64(m.Runs)
	}
	if m.Started > 0 {
		m.InterventionPct = float64(m.Blocked) / float64(m.Started)
	}
	return m
}

func doneTime(t model.Task) time.Time {
	for i := len(t.History) - 1; i >= 0; i-- {
		if t.History[i].Stage == model.StageDone {
			return t.History[i].At
		}
	}
	return t.UpdatedAt
}
