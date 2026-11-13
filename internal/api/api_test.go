package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"conduit/internal/api"
	"conduit/internal/auth"
	"conduit/internal/engine"
	"conduit/internal/model"
	"conduit/internal/secrets"
	"conduit/internal/seed"
	"conduit/internal/store"
	"conduit/internal/worker"
)

// startWorkers runs a worker pool for a test store+engine so enqueued runs
// execute, stopping it when the test ends.
func startWorkers(t *testing.T, st store.Store, eng *engine.Engine) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	p := worker.New(st, eng, 2)
	p.PollInterval = 10 * time.Millisecond
	p.Start(ctx)
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	st := store.NewMem()
	if err := st.PutWorkflow(seed.Workflow()); err != nil {
		t.Fatal(err)
	}
	eng := engine.New(st)
	eng.StepDelay = 0
	eng.TriggerDelay = 0
	startWorkers(t, st, eng)
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
	// Cancel is idempotent: cancelling an already-finished run returns 200 so the
	// UI always leaves the running state cleanly (it reports the run's status).
	if code := postJSON(t, ts.URL+"/api/runs/"+run.ID+"/cancel", map[string]any{}, nil); code != 200 {
		t.Errorf("second cancel = %d, want 200 (idempotent)", code)
	}
	// Cancelling an unknown run is still a 404.
	if code := postJSON(t, ts.URL+"/api/runs/run_does_not_exist/cancel", map[string]any{}, nil); code != http.StatusNotFound {
		t.Errorf("cancel unknown run = %d, want 404", code)
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

func TestCreateRunAcceptsExplicitInput(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	var run model.Run
	code := postJSON(t, ts.URL+"/api/runs", map[string]any{
		"workflow_id": "wf_preview_envs",
		"input":       map[string]any{"action": "opened", "number": 154},
	}, &run)
	if code != http.StatusCreated {
		t.Fatalf("create run status = %d, want 201", code)
	}
	if run.Input["action"] != "opened" || run.Input["number"] != float64(154) {
		t.Fatalf("run input = %#v", run.Input)
	}
}

func getRunRaw(t *testing.T, url string) model.Run {
	t.Helper()
	var r model.Run
	getJSON(t, url, &r)
	return r
}

func TestProjectScoping(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// The seeded workflow lives in the default project; it is NOT visible under
	// another project header.
	get := func(path, project string) (int, []byte) {
		req, _ := http.NewRequest("GET", ts.URL+path, nil)
		if project != "" {
			req.Header.Set("X-Conduit-Project", project)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, b
	}

	var def []model.Workflow
	_, body := get("/api/workflows", "")
	_ = json.Unmarshal(body, &def)
	if len(def) != 1 {
		t.Fatalf("default project workflows = %d, want 1", len(def))
	}

	var other []model.Workflow
	_, body = get("/api/workflows", "empty-project")
	_ = json.Unmarshal(body, &other)
	if len(other) != 0 {
		t.Errorf("empty project workflows = %d, want 0 (isolation)", len(other))
	}

	// The seeded workflow is 404 when accessed under the wrong project.
	code, _ := get("/api/workflows/wf_preview_envs", "empty-project")
	if code != http.StatusNotFound {
		t.Errorf("cross-project get = %d, want 404", code)
	}
	code, _ = get("/api/workflows/wf_preview_envs", "")
	if code != 200 {
		t.Errorf("same-project get = %d, want 200", code)
	}
}

func TestRateLimiting(t *testing.T) {
	st := store.NewMem()
	_ = st.PutWorkflow(seed.Workflow())
	srv := &api.Server{Store: st, Engine: engine.New(st), Limiter: api.NewRateLimiter(0, 3), Web: fstest.MapFS{}}
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	// With rate 0 and burst 3, the 4th request within the window is limited.
	codes := map[int]int{}
	for i := 0; i < 6; i++ {
		resp, err := http.Get(ts.URL + "/api/health")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		codes[resp.StatusCode]++
	}
	if codes[http.StatusTooManyRequests] == 0 {
		t.Errorf("expected some 429s, got %v", codes)
	}
}

func TestAuditLog(t *testing.T) {
	ts := newTestServer(t) // open mode (no auth) -> callerIsGlobalAdmin true
	defer ts.Close()

	// A mutation is recorded.
	postJSON(t, ts.URL+"/api/runs", map[string]string{"workflow_id": "wf_preview_envs"}, nil)
	// Audit writes are best-effort (async); poll briefly.
	var entries []model.AuditEntry
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		getJSON(t, ts.URL+"/api/audit", &entries)
		if len(entries) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	found := false
	for _, e := range entries {
		if e.Method == "POST" && e.Path == "/api/runs" {
			found = true
		}
	}
	if !found {
		t.Errorf("audit did not record POST /api/runs: %+v", entries)
	}
}

func TestProjectMembershipIsolation(t *testing.T) {
	st := store.NewMem()
	_ = st.CreateProject(model.Project{ID: "default", Name: "Default", CreatedAt: time.Now()})
	_ = st.CreateProject(model.Project{ID: "secret-proj", Name: "Secret", CreatedAt: time.Now()})
	_ = st.PutWorkflow(model.Workflow{ID: "w", ProjectID: "secret-proj", Name: "W",
		Nodes: []model.Node{{ID: "t", Type: "github", Kind: model.KindTrigger}}})

	authSvc := auth.New(st, "s")
	// A regular member-role user who is NOT a member of secret-proj.
	u, _ := authSvc.CreateUser("bob@x.com", "Bob", "pw", model.RoleMember)
	tok, _ := authSvc.IssueJWT(u)

	srv := &api.Server{Store: st, Engine: engine.New(st), Auth: authSvc, Web: fstest.MapFS{}}
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	req := func(project string) int {
		r, _ := http.NewRequest("GET", ts.URL+"/api/workflows", nil)
		r.Header.Set("Authorization", "Bearer "+tok)
		r.Header.Set("X-Conduit-Project", project)
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	// Not a member of secret-proj → 403.
	if c := req("secret-proj"); c != http.StatusForbidden {
		t.Errorf("non-member access = %d, want 403", c)
	}
	// Add as a member → now allowed.
	if err := st.AddProjectMember(model.ProjectMember{ProjectID: "secret-proj", UserID: u.ID, Role: model.RoleMember, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if c := req("secret-proj"); c != 200 {
		t.Errorf("member access = %d, want 200", c)
	}
}

func TestListProjectsFiltersByMembership(t *testing.T) {
	st := store.NewMem()
	_ = st.CreateProject(model.Project{ID: "default", Name: "Default", CreatedAt: time.Now()})
	_ = st.CreateProject(model.Project{ID: "ai-factory", Name: "AI Factory", CreatedAt: time.Now()})
	_ = st.CreateProject(model.Project{ID: "mine", Name: "Mine", CreatedAt: time.Now()})

	authSvc := auth.New(st, "s")
	member, _ := authSvc.CreateUser("m@x.com", "M", "pw", model.RoleMember)
	_ = st.AddProjectMember(model.ProjectMember{ProjectID: "mine", UserID: member.ID, Role: model.RoleOwner, CreatedAt: time.Now()})
	memberTok, _ := authSvc.IssueJWT(member)

	admin, _ := authSvc.CreateUser("a@x.com", "A", "pw", model.RoleAdmin)
	adminTok, _ := authSvc.IssueJWT(admin)

	srv := &api.Server{Store: st, Engine: engine.New(st), Auth: authSvc, Web: fstest.MapFS{}}
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	list := func(tok string) []model.Project {
		r, _ := http.NewRequest("GET", ts.URL+"/api/projects", nil)
		r.Header.Set("Authorization", "Bearer "+tok)
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var ps []model.Project
		_ = json.NewDecoder(resp.Body).Decode(&ps)
		return ps
	}

	// The member sees only their own project.
	ms := list(memberTok)
	if len(ms) != 1 || ms[0].ID != "mine" {
		t.Errorf("member projects = %v, want [mine]", ms)
	}
	// The global admin sees all three.
	as := list(adminTok)
	if len(as) != 3 {
		t.Errorf("admin projects = %d, want 3", len(as))
	}
}

func TestAuthMiddleware(t *testing.T) {
	st := store.NewMem()
	if err := st.PutWorkflow(seed.Workflow()); err != nil {
		t.Fatal(err)
	}
	authSvc := auth.New(st, "test-secret")
	admin, err := authSvc.CreateUser("admin@x.com", "Admin", "pw", model.RoleOwner)
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := authSvc.CreateUser("view@x.com", "View", "pw", model.RoleViewer)
	if err != nil {
		t.Fatal(err)
	}
	// The viewer is a member of the default project (viewer role); the admin is a
	// global owner and needs no membership.
	if err := st.AddProjectMember(model.ProjectMember{ProjectID: model.DefaultProjectID, UserID: viewer.ID, Role: model.RoleViewer, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	srv := &api.Server{Store: st, Engine: engine.New(st), Auth: authSvc, Web: fstest.MapFS{}}
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	get := func(path, tok string) int {
		req, _ := http.NewRequest("GET", ts.URL+path, nil)
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	post := func(path, tok string, body any) int {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", ts.URL+path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	// Health and login are public.
	if c := get("/api/health", ""); c != 200 {
		t.Errorf("health without auth = %d, want 200", c)
	}
	// Protected read without a token → 401.
	if c := get("/api/workflows", ""); c != 401 {
		t.Errorf("workflows without auth = %d, want 401", c)
	}
	// Login to get a JWT.
	adminTok, _ := authSvc.IssueJWT(admin)
	viewerTok, _ := authSvc.IssueJWT(viewer)

	if c := get("/api/workflows", adminTok); c != 200 {
		t.Errorf("workflows with admin token = %d, want 200", c)
	}
	if c := get("/api/workflows", viewerTok); c != 200 {
		t.Errorf("viewer read = %d, want 200", c)
	}
	// Viewer cannot write.
	if c := post("/api/runs", viewerTok, map[string]string{"workflow_id": "wf_preview_envs"}); c != 403 {
		t.Errorf("viewer write = %d, want 403", c)
	}
	// Admin can write.
	if c := post("/api/runs", adminTok, map[string]string{"workflow_id": "wf_preview_envs"}); c != 201 {
		t.Errorf("admin write = %d, want 201", c)
	}
	// Bad token → 401.
	if c := get("/api/workflows", "garbage"); c != 401 {
		t.Errorf("garbage token = %d, want 401", c)
	}

	// Webhooks require an API token.
	if c := post("/api/hooks/wf_preview_envs", "", nil); c != 401 {
		t.Errorf("hook without token = %d, want 401", c)
	}
	plain, _, _ := authSvc.CreateAPIToken("ci", model.RoleMember)
	if c := post("/api/hooks/wf_preview_envs", plain, map[string]any{"x": 1}); c != 201 {
		t.Errorf("hook with API token = %d, want 201", c)
	}
}

func TestLoginEndpoint(t *testing.T) {
	st := store.NewMem()
	authSvc := auth.New(st, "s")
	if _, err := authSvc.CreateUser("a@b.com", "A", "pw", model.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	srv := &api.Server{Store: st, Engine: engine.New(st), Auth: authSvc, Web: fstest.MapFS{}}
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	var out struct {
		Token string     `json:"token"`
		User  model.User `json:"user"`
	}
	if c := postJSON(t, ts.URL+"/api/auth/login", map[string]string{"email": "a@b.com", "password": "pw"}, &out); c != 200 {
		t.Fatalf("login = %d, want 200", c)
	}
	if out.Token == "" || out.User.Email != "a@b.com" {
		t.Errorf("login response: token empty=%v user=%+v", out.Token == "", out.User)
	}
	// Password must never be serialized.
	if out.User.PasswordHash != "" {
		t.Error("password hash leaked in login response")
	}
	// Wrong password → 401.
	if c := postJSON(t, ts.URL+"/api/auth/login", map[string]string{"email": "a@b.com", "password": "nope"}, nil); c != 401 {
		t.Errorf("bad login = %d, want 401", c)
	}
}

func TestRegisterEndpoint(t *testing.T) {
	st := store.NewMem()
	authSvc := auth.New(st, "s")
	srv := &api.Server{Store: st, Engine: engine.New(st), Auth: authSvc, Web: fstest.MapFS{}, AllowSignup: true}
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	var out struct {
		Token   string            `json:"token"`
		User    model.User        `json:"user"`
		Project map[string]string `json:"project"`
	}
	if c := postJSON(t, ts.URL+"/api/auth/register",
		map[string]string{"email": "new@user.com", "name": "New", "password": "password123"}, &out); c != 201 {
		t.Fatalf("register = %d, want 201", c)
	}
	if out.Token == "" || out.User.Email != "new@user.com" || out.User.Role != model.RoleMember {
		t.Errorf("register response: token empty=%v user=%+v", out.Token == "", out.User)
	}
	if out.User.PasswordHash != "" {
		t.Error("password hash leaked in register response")
	}
	// A personal project was created and the user owns it.
	if out.Project["id"] == "" {
		t.Fatal("register did not return a personal project")
	}
	if _, ok, _ := st.GetProject(out.Project["id"]); !ok {
		t.Error("personal project was not created")
	}

	// Short password → 400.
	if c := postJSON(t, ts.URL+"/api/auth/register",
		map[string]string{"email": "x@y.com", "password": "short"}, nil); c != 400 {
		t.Errorf("short password = %d, want 400", c)
	}
	// Duplicate email → 409.
	if c := postJSON(t, ts.URL+"/api/auth/register",
		map[string]string{"email": "new@user.com", "password": "password123"}, nil); c != 409 {
		t.Errorf("duplicate email = %d, want 409", c)
	}

	// Signup disabled → 403.
	srv2 := &api.Server{Store: store.NewMem(), Engine: engine.New(st), Auth: auth.New(store.NewMem(), "s"), Web: fstest.MapFS{}, AllowSignup: false}
	ts2 := httptest.NewServer(srv2.Routes())
	defer ts2.Close()
	if c := postJSON(t, ts2.URL+"/api/auth/register",
		map[string]string{"email": "z@y.com", "password": "password123"}, nil); c != 403 {
		t.Errorf("signup disabled = %d, want 403", c)
	}
}

func TestSecretsAPI(t *testing.T) {
	st := store.NewMem()
	if err := st.PutWorkflow(seed.Workflow()); err != nil {
		t.Fatal(err)
	}
	mgr, _, err := secrets.NewManager(st, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	srv := &api.Server{Store: st, Engine: engine.New(st), Secrets: mgr, Web: fstest.MapFS{}}
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	// Set a secret.
	if code := putJSON(t, ts.URL+"/api/secrets/slack_url", map[string]string{"value": "https://hooks.example/abc"}, nil); code != 200 {
		t.Fatalf("put secret = %d, want 200", code)
	}
	// List returns names only — never the value.
	var names []string
	getJSON(t, ts.URL+"/api/secrets", &names)
	if len(names) != 1 || names[0] != "slack_url" {
		t.Fatalf("list secrets = %v, want [slack_url]", names)
	}
	// There is no GET-value endpoint; confirm the value isn't exposed via list.
	for _, n := range names {
		if n == "https://hooks.example/abc" {
			t.Fatal("secret value leaked in list")
		}
	}
	// Delete it.
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/secrets/slack_url", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("delete secret: err=%v code=%d", err, resp.StatusCode)
	}
	resp.Body.Close()
	getJSON(t, ts.URL+"/api/secrets", &names)
	if len(names) != 0 {
		t.Errorf("after delete, secrets = %v, want empty", names)
	}
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

func TestOAuthConfigAndStart(t *testing.T) {
	st := store.NewMem()
	authSvc := auth.New(st, "s")
	providers := api.NewOAuthProviders("gid", "gsecret", "", "") // only google configured
	srv := &api.Server{Store: st, Engine: engine.New(st), Auth: authSvc, Web: fstest.MapFS{}, AllowSignup: true, OAuth: providers}
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	// config advertises only the configured provider.
	var cfg struct {
		Providers []string `json:"providers"`
	}
	getJSON(t, ts.URL+"/api/auth/config", &cfg)
	if len(cfg.Providers) != 1 || cfg.Providers[0] != "google" {
		t.Fatalf("providers = %v, want [google]", cfg.Providers)
	}

	// start redirects (302) to Google's authorize URL for a configured provider.
	noRedir := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := noRedir.Get(ts.URL + "/api/auth/oauth/google/start")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("start status = %d, want 302", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.Contains(loc, "accounts.google.com") || !strings.Contains(loc, "state=") || !strings.Contains(loc, "client_id=gid") {
		t.Errorf("unexpected authorize redirect: %s", loc)
	}

	// an unconfigured provider is 404.
	resp2, _ := noRedir.Get(ts.URL + "/api/auth/oauth/github/start")
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Errorf("github start = %d, want 404", resp2.StatusCode)
	}
}

func TestDeleteProject(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// Create a project, then delete it.
	var created struct {
		ID string `json:"id"`
	}
	if code := postJSON(t, ts.URL+"/api/projects", map[string]string{"name": "Scratch"}, &created); code != 201 {
		t.Fatalf("create project = %d", code)
	}
	del := func(path string) int {
		req, _ := http.NewRequest(http.MethodDelete, ts.URL+path, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("DELETE %s: %v", path, err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if code := del("/api/projects/" + created.ID); code != 200 {
		t.Errorf("delete created project = %d, want 200", code)
	}
	// It's gone.
	var list []map[string]any
	getJSON(t, ts.URL+"/api/projects", &list)
	for _, p := range list {
		if p["id"] == created.ID {
			t.Error("deleted project still listed")
		}
	}
	// Built-in projects are protected.
	if code := del("/api/projects/default"); code != 400 {
		t.Errorf("delete built-in 'default' = %d, want 400", code)
	}
	// Unknown project → 404.
	if code := del("/api/projects/nope"); code != 404 {
		t.Errorf("delete unknown = %d, want 404", code)
	}
}

func TestReviewRequestsFlow(t *testing.T) {
	st := store.NewMem()
	_ = st.CreateProject(model.Project{ID: seed.GitHubProjectID, Name: "GitHub"})
	_ = st.PutWorkflow(seed.GitHubWorkflow())
	eng := engine.New(st)
	eng.StepDelay, eng.TriggerDelay = 0, 0
	startWorkers(t, st, eng)
	srv := &api.Server{Store: st, Engine: eng, Web: fstest.MapFS{}}
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	rr, _ := st.CreateReviewRequest(model.ReviewRequest{
		ID: "rr_1", ProjectID: seed.GitHubProjectID, WorkflowID: seed.GitHubWorkflowID,
		Owner: "VITCS", Repo: "fitvive-landing-page", Number: 7, Head: "conduit/x",
		URL: "https://github.com/VITCS/fitvive-landing-page/pull/7", Title: "feat: x", Status: "pending",
	})

	do := func(method, path string) (int, map[string]any) {
		req, _ := http.NewRequest(method, ts.URL+path, nil)
		req.Header.Set("X-Conduit-Project", seed.GitHubProjectID)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		var m map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&m)
		return resp.StatusCode, m
	}

	// List shows the pending request.
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/review-requests", nil)
	req.Header.Set("X-Conduit-Project", seed.GitHubProjectID)
	resp, _ := http.DefaultClient.Do(req)
	var list []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) != 1 || list[0]["id"] != "rr_1" {
		t.Fatalf("list = %v, want 1 pending", list)
	}

	// Run it → starts a review run and marks the request started.
	code, m := do(http.MethodPost, "/api/review-requests/"+rr.ID+"/run")
	if code != 200 || m["run"] == nil {
		t.Fatalf("run review = %d (%v)", code, m)
	}
	got, _, _ := st.GetReviewRequest("rr_1")
	if got.Status != "started" || got.RunID == "" {
		t.Errorf("after run: status=%s runID=%s", got.Status, got.RunID)
	}

	// It no longer appears in the pending list.
	req2, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/review-requests", nil)
	req2.Header.Set("X-Conduit-Project", seed.GitHubProjectID)
	resp2, _ := http.DefaultClient.Do(req2)
	var list2 []map[string]any
	_ = json.NewDecoder(resp2.Body).Decode(&list2)
	resp2.Body.Close()
	if len(list2) != 0 {
		t.Errorf("pending after run = %d, want 0", len(list2))
	}

	// Dismiss a second request.
	_, _ = st.CreateReviewRequest(model.ReviewRequest{ID: "rr_2", ProjectID: seed.GitHubProjectID, WorkflowID: seed.GitHubWorkflowID, Owner: "o", Repo: "r", Number: 9, Status: "pending"})
	if code, _ := do(http.MethodPost, "/api/review-requests/rr_2/dismiss"); code != 200 {
		t.Errorf("dismiss = %d", code)
	}
	got2, _, _ := st.GetReviewRequest("rr_2")
	if got2.Status != "dismissed" {
		t.Errorf("rr_2 status = %s, want dismissed", got2.Status)
	}
}

func TestCreateRunConsumesPendingReviewRequestForGitHubWorkflow(t *testing.T) {
	st := store.NewMem()
	_ = st.CreateProject(model.Project{ID: seed.GitHubProjectID, Name: "GitHub"})
	_ = st.PutWorkflow(seed.GitHubWorkflow())
	eng := engine.New(st)
	eng.StepDelay, eng.TriggerDelay = 0, 0
	srv := &api.Server{Store: st, Engine: eng, Web: fstest.MapFS{}}
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	rr, _ := st.CreateReviewRequest(model.ReviewRequest{
		ID: "rr_top_run", ProjectID: seed.GitHubProjectID, WorkflowID: seed.GitHubWorkflowID,
		Owner: "VITCS", Repo: "fitvive-landing-page", Number: 42, Head: "conduit/ENG-1",
		URL: "https://github.com/VITCS/fitvive-landing-page/pull/42", Title: "feat: ENG-1", Status: "pending",
	})

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/runs", strings.NewReader(`{"workflow_id":"wf_github"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Conduit-Project", seed.GitHubProjectID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/runs: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create github run = %d, want 201", resp.StatusCode)
	}
	var run model.Run
	if err := json.NewDecoder(resp.Body).Decode(&run); err != nil {
		t.Fatal(err)
	}
	if run.Title != "review: fitvive-landing-page #42" {
		t.Errorf("run title = %q", run.Title)
	}
	if run.Input["action"] != "opened" || run.Input["number"] != float64(42) {
		t.Fatalf("run input did not carry PR event: %#v", run.Input)
	}
	repo, _ := run.Input["repository"].(map[string]any)
	owner, _ := repo["owner"].(map[string]any)
	if repo["name"] != "fitvive-landing-page" || owner["login"] != "VITCS" {
		t.Errorf("repo input = %#v", repo)
	}
	pr, _ := run.Input["pull_request"].(map[string]any)
	head, _ := pr["head"].(map[string]any)
	if head["ref"] != "conduit/ENG-1" {
		t.Errorf("head ref = %#v", head)
	}
	got, _, _ := st.GetReviewRequest(rr.ID)
	if got.Status != "started" || got.RunID != run.ID {
		t.Errorf("request not marked started: status=%s runID=%s want %s", got.Status, got.RunID, run.ID)
	}
}
