package api_test

import (
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"conduit/internal/api"
	"conduit/internal/engine"
	"conduit/internal/model"
	"conduit/internal/store"
)

func TestNeedsYouListsQuestionsAndApprovalsAndAnswers(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()
	wf := model.Workflow{ID: "wf_q", ProjectID: model.DefaultProjectID, Name: "Ask",
		Nodes: []model.Node{
			{ID: "t1", Type: "start", Kind: model.KindTrigger, Title: "Start"},
			{ID: "ag", Type: "agent", Kind: model.KindStep, Title: "Refine & Plan",
				Config: []model.Field{{Key: "prompt", Value: "Plan"}, {Key: "question", Value: "Which region?"},
					{Key: "question_options", Value: "EU|US"}}},
			{ID: "done", Type: "noop", Kind: model.KindStep, Title: "Done"},
		},
		Edges: []model.Edge{{From: "t1", To: "ag"}, {From: "ag", To: "done", Label: "success"}}}
	if code := putJSON(t, ts.URL+"/api/workflows/wf_q", wf, nil); code != 200 {
		t.Fatalf("put workflow = %d", code)
	}
	var qrun, arun model.Run
	postJSON(t, ts.URL+"/api/runs", map[string]string{"workflow_id": "wf_q", "title": "Ask run"}, &qrun)
	postJSON(t, ts.URL+"/api/runs", map[string]string{"workflow_id": "wf_preview_envs"}, &arun)
	waitHTTP(t, ts.URL+"/api/runs/"+qrun.ID, model.StatusWaiting)
	waitHTTP(t, ts.URL+"/api/runs/"+arun.ID, model.StatusWaiting)

	var att api.Attention
	getJSON(t, ts.URL+"/api/attention", &att)
	if att.Total != 2 || att.ByProject[model.DefaultProjectID] != 2 {
		t.Fatalf("attention = %+v", att)
	}
	q := att.Items[0] // questions come first
	if q.Kind != "question" || q.RunID != qrun.ID || q.NodeID != "ag" || q.Step != "Refine & Plan" ||
		q.Text != "Which region?" || len(q.Options) != 2 || q.Title != "Ask run" {
		t.Fatalf("question item = %+v", q)
	}
	if a := att.Items[1]; a.Kind != "approval" || a.RunID != arun.ID || a.NodeID != "a1" {
		t.Fatalf("approval item = %+v", a)
	}

	// An answer is required, and only a question step takes one.
	if code := postJSON(t, ts.URL+"/api/runs/"+qrun.ID+"/answer", map[string]string{"node_id": "ag", "answer": "  "}, nil); code != 400 {
		t.Errorf("empty answer = %d, want 400", code)
	}
	if code := postJSON(t, ts.URL+"/api/runs/"+arun.ID+"/answer", map[string]string{"node_id": "a1", "answer": "EU"}, nil); code != 409 {
		t.Errorf("answering an approval gate = %d, want 409", code)
	}
	if code := postJSON(t, ts.URL+"/api/runs/"+qrun.ID+"/answer", map[string]string{"node_id": "ag", "answer": "EU"}, nil); code != 200 {
		t.Fatalf("answer = %d", code)
	}
	final := waitHTTP(t, ts.URL+"/api/runs/"+qrun.ID, model.StatusSuccess)
	if qa, _ := final.Memory["answers_for_ag"].([]any); len(qa) != 1 {
		t.Fatalf("answers = %v", final.Memory["answers_for_ag"])
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		getJSON(t, ts.URL+"/api/attention", &att)
		if att.Total == 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if att.Total != 1 || att.Items[0].Kind != "approval" {
		t.Fatalf("after answering, attention = %+v", att)
	}
}

func TestNeedsYouListsStalledAndNotStartedRuns(t *testing.T) {
	st := store.NewMem()
	eng := engine.New(st)
	srv := &api.Server{Store: st, Engine: eng, Web: fstest.MapFS{}}
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()
	wf := model.Workflow{ID: "wf_s", ProjectID: model.DefaultProjectID, Name: "S",
		Nodes: []model.Node{{ID: "apply", Type: "noop", Kind: model.KindStep, Title: "Apply changes"}}}
	_ = st.PutWorkflow(wf)
	since := time.Now().Add(-12 * time.Minute).UTC()
	_, _ = st.CreateRun(&model.Run{ID: "r_stalled", WorkflowID: "wf_s", ProjectID: model.DefaultProjectID, Title: "ENG-9",
		Status: model.StatusRunning, CreatedAt: since,
		Steps: map[string]*model.StepRun{"apply": {NodeID: "apply", Status: model.StatusRunning,
			Output: map[string]any{"stall": "stuck", "stall_since": since.Format(time.RFC3339)}}}})
	_, _ = st.CreateRun(&model.Run{ID: "r_slow", WorkflowID: "wf_s", ProjectID: model.DefaultProjectID, Title: "ENG-10",
		Status: model.StatusRunning, CreatedAt: since,
		Steps: map[string]*model.StepRun{"apply": {NodeID: "apply", Status: model.StatusRunning, Output: map[string]any{"stall": "slow"}}}})
	_, _ = st.CreateRun(&model.Run{ID: "r_queued", WorkflowID: "wf_s", ProjectID: model.DefaultProjectID, Title: "ENG-11",
		Status: model.StatusQueued, CreatedAt: since, Steps: map[string]*model.StepRun{}})
	_, _ = st.CreateRun(&model.Run{ID: "r_fresh", WorkflowID: "wf_s", ProjectID: model.DefaultProjectID, Title: "ENG-12",
		Status: model.StatusQueued, CreatedAt: time.Now(), Steps: map[string]*model.StepRun{}})

	var att api.Attention
	getJSON(t, ts.URL+"/api/attention", &att)
	got := map[string]api.AttentionItem{}
	for _, it := range att.Items {
		got[it.RunID] = it
	}
	if att.Total != 2 {
		t.Fatalf("only the stuck step and the old queued run belong in Needs you: %+v", att.Items)
	}
	if s := got["r_stalled"]; s.Kind != "stuck" || s.Reason != "stalled" || s.Step != "Apply changes" || s.NodeID != "apply" ||
		!strings.Contains(s.Text, "no progress for 12 min") {
		t.Errorf("stalled item = %+v", s)
	}
	if q := got["r_queued"]; q.Kind != "stuck" || q.Reason != "not_started" {
		t.Errorf("queued item = %+v", q)
	}
}
