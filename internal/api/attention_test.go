package api_test

import (
	"testing"
	"time"

	"conduit/internal/api"
	"conduit/internal/model"
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
