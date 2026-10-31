package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"conduit/internal/api"
	"conduit/internal/engine"
	"conduit/internal/model"
	"conduit/internal/seed"
	"conduit/internal/store"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	st := store.NewMem()
	if err := st.PutWorkflow(seed.Workflow()); err != nil {
		t.Fatal(err)
	}
	eng := engine.New(st)
	eng.StepDelay = 0
	eng.TriggerDelay = 0
	srv := &api.Server{Store: st, Engine: eng, Web: fstest.MapFS{}}
	return httptest.NewServer(srv.Routes())
}

func getJSON(t *testing.T, url string, v any) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if v != nil {
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			t.Fatalf("decode %s: %v", url, err)
		}
	}
	return resp.StatusCode
}

func postJSON(t *testing.T, url string, body any, v any) int {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	if v != nil {
		_ = json.NewDecoder(resp.Body).Decode(v)
	} else {
		io.Copy(io.Discard, resp.Body)
	}
	return resp.StatusCode
}

func TestHealth(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()
	var body map[string]any
	if code := getJSON(t, ts.URL+"/api/health", &body); code != 200 {
		t.Fatalf("health status = %d", code)
	}
	if body["status"] != "ok" {
		t.Errorf("health body = %v", body)
	}
}

func TestListWorkflows(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()
	var wfs []model.Workflow
	if code := getJSON(t, ts.URL+"/api/workflows", &wfs); code != 200 {
		t.Fatalf("status = %d", code)
	}
	if len(wfs) != 1 || wfs[0].ID != "wf_preview_envs" {
		t.Fatalf("workflows = %+v", wfs)
	}
}

func TestRunLifecycleOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// start a run
	var run model.Run
	if code := postJSON(t, ts.URL+"/api/runs", map[string]string{"workflow_id": "wf_preview_envs"}, &run); code != 201 {
		t.Fatalf("create run status = %d", code)
	}
	if run.ID == "" {
		t.Fatal("run ID empty")
	}

	// poll until it is waiting at the gate
	waitHTTP(t, ts.URL+"/api/runs/"+run.ID, model.StatusWaiting)

	// approve
	if code := postJSON(t, ts.URL+"/api/runs/"+run.ID+"/approve",
		map[string]string{"node_id": "a1", "decision": "approved"}, nil); code != 200 {
		t.Fatalf("approve status = %d", code)
	}

	// poll until success
	final := waitHTTP(t, ts.URL+"/api/runs/"+run.ID, model.StatusSuccess)
	if final.Steps["u1"].Status != model.StatusSuccess {
		t.Errorf("u1 = %s, want success", final.Steps["u1"].Status)
	}
	if _, ok := final.Memory["preview-envs/154"]; !ok {
		t.Error("memory not persisted through the API")
	}
}

func TestCancelRunOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	var run model.Run
	postJSON(t, ts.URL+"/api/runs", map[string]string{"workflow_id": "wf_preview_envs"}, &run)
	waitHTTP(t, ts.URL+"/api/runs/"+run.ID, model.StatusWaiting)

	if code := postJSON(t, ts.URL+"/api/runs/"+run.ID+"/cancel", map[string]any{}, nil); code != 200 {
		t.Fatalf("cancel status = %d, want 200", code)
	}
	final := waitHTTP(t, ts.URL+"/api/runs/"+run.ID, model.StatusCanceled)
	if final.Status != model.StatusCanceled {
		t.Errorf("run = %s, want canceled", final.Status)
	}
	// Cancelling a finished run → 409.
	if code := postJSON(t, ts.URL+"/api/runs/"+run.ID+"/cancel", map[string]any{}, nil); code != http.StatusConflict {
		t.Errorf("second cancel = %d, want 409", code)
	}
}

func TestWebhookStartsRunWithInput(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	var run model.Run
	code := postJSON(t, ts.URL+"/api/hooks/wf_preview_envs", map[string]any{"pr": 154, "action": "opened"}, &run)
	if code != http.StatusCreated {
		t.Fatalf("hook status = %d, want 201", code)
	}
	if run.ID == "" {
		t.Fatal("hook did not return a run")
	}
	// The trigger node should expose the posted body as input.
	waitHTTP(t, ts.URL+"/api/runs/"+run.ID, model.StatusWaiting)
	full := getRunRaw(t, ts.URL+"/api/runs/"+run.ID)
	out := full.Steps["t1"].Output
	in, ok := out["input"].(map[string]any)
	if !ok || in["action"] != "opened" {
		t.Errorf("trigger input not bound from webhook body: %v", out)
	}

	// Unknown workflow → 404.
	if code := postJSON(t, ts.URL+"/api/hooks/nope", map[string]any{}, nil); code != http.StatusNotFound {
		t.Errorf("hook unknown workflow = %d, want 404", code)
	}
}

func getRunRaw(t *testing.T, url string) model.Run {
	t.Helper()
	var r model.Run
	getJSON(t, url, &r)
	return r
}

func TestApproveNothingWaiting(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()
	code := postJSON(t, ts.URL+"/api/runs/run_missing/approve",
		map[string]string{"node_id": "a1", "decision": "approved"}, nil)
	if code != http.StatusConflict {
		t.Errorf("approve with nothing waiting = %d, want 409", code)
	}
}

func TestPutWorkflowPersistsEdits(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// Load, edit (rename + add a node + an edge), and save via PUT.
	var wf model.Workflow
	getJSON(t, ts.URL+"/api/workflows/wf_preview_envs", &wf)
	wf.Name = "Edited name"
	wf.Nodes = append(wf.Nodes, model.Node{ID: "nx", Type: "http", Kind: model.KindStep, Title: "Extra"})
	wf.Edges = append(wf.Edges, model.Edge{From: "u1", To: "nx"})

	var saved model.Workflow
	if code := putJSON(t, ts.URL+"/api/workflows/wf_preview_envs", wf, &saved); code != 200 {
		t.Fatalf("PUT status = %d", code)
	}

	// Read it back fresh — the edits must have persisted.
	var reread model.Workflow
	getJSON(t, ts.URL+"/api/workflows/wf_preview_envs", &reread)
	if reread.Name != "Edited name" {
		t.Errorf("name = %q, want edited", reread.Name)
	}
	if !hasNode(reread, "nx") {
		t.Error("added node nx not persisted")
	}
	found := false
	for _, e := range reread.Edges {
		if e.From == "u1" && e.To == "nx" {
			found = true
		}
	}
	if !found {
		t.Error("added edge u1->nx not persisted")
	}
}

func TestPutInvalidWorkflowRejected(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// A workflow with a dangling edge must be rejected with 422, and the stored
	// copy must stay unchanged.
	var wf model.Workflow
	getJSON(t, ts.URL+"/api/workflows/wf_preview_envs", &wf)
	wf.Edges = append(wf.Edges, model.Edge{From: "u1", To: "ghost"})

	var resp struct {
		Error   string   `json:"error"`
		Details []string `json:"details"`
	}
	if code := putJSON(t, ts.URL+"/api/workflows/wf_preview_envs", wf, &resp); code != http.StatusUnprocessableEntity {
		t.Fatalf("PUT invalid status = %d, want 422", code)
	}
	if len(resp.Details) == 0 {
		t.Error("expected validation details in response")
	}

	// The persisted workflow must not have gained the bad edge.
	var reread model.Workflow
	getJSON(t, ts.URL+"/api/workflows/wf_preview_envs", &reread)
	for _, e := range reread.Edges {
		if e.To == "ghost" {
			t.Fatal("invalid edge was persisted despite 422")
		}
	}
}

func hasNode(wf model.Workflow, id string) bool {
	for _, n := range wf.Nodes {
		if n.ID == id {
			return true
		}
	}
	return false
}

func putJSON(t *testing.T, url string, body any, v any) int {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPut, url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", url, err)
	}
	defer resp.Body.Close()
	if v != nil {
		_ = json.NewDecoder(resp.Body).Decode(v)
	} else {
		io.Copy(io.Discard, resp.Body)
	}
	return resp.StatusCode
}

func TestGetUnknownRun(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()
	if code := getJSON(t, ts.URL+"/api/runs/nope", nil); code != http.StatusNotFound {
		t.Errorf("unknown run = %d, want 404", code)
	}
}

func waitHTTP(t *testing.T, url string, want model.Status) model.Run {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var r model.Run
		if getJSON(t, url, &r) == 200 && r.Status == want {
			return r
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run did not reach %s within timeout", want)
	return model.Run{}
}
