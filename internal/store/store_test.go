package store

import (
	"os"
	"testing"
	"time"

	"conduit/internal/model"
)

// storeContract exercises the behavior every Store implementation must provide.
// It runs against the in-memory store always and against Postgres when
// CONDUIT_TEST_POSTGRES is set to a DSN.
func storeContract(t *testing.T, s Store) {
	t.Helper()

	// workflows: put / get / list
	wf := model.Workflow{ID: "wf1", Name: "Demo",
		Nodes: []model.Node{{ID: "a", Type: "http", Title: "Call"}},
		Edges: []model.Edge{{From: "a", To: "a"}}}
	if err := s.PutWorkflow(wf); err != nil {
		t.Fatalf("PutWorkflow: %v", err)
	}
	got, ok, err := s.GetWorkflow("wf1")
	if err != nil || !ok {
		t.Fatalf("GetWorkflow: ok=%v err=%v", ok, err)
	}
	if got.Name != "Demo" || len(got.Nodes) != 1 {
		t.Fatalf("workflow round-trip mismatch: %+v", got)
	}
	if _, ok, _ := s.GetWorkflow("missing"); ok {
		t.Error("GetWorkflow(missing) returned ok=true")
	}
	wfs, err := s.ListWorkflows("")
	if err != nil || len(wfs) == 0 {
		t.Fatalf("ListWorkflows: n=%d err=%v", len(wfs), err)
	}

	// projects: scoping isolates workflows by project.
	if err := s.CreateProject(model.Project{ID: "pa", Name: "A", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	_ = s.CreateProject(model.Project{ID: "pb", Name: "B", CreatedAt: time.Now()})
	if err := s.PutWorkflow(model.Workflow{ID: "wa", ProjectID: "pa", Name: "WA"}); err != nil {
		t.Fatal(err)
	}
	_ = s.PutWorkflow(model.Workflow{ID: "wb", ProjectID: "pb", Name: "WB"})
	inA, _ := s.ListWorkflows("pa")
	if len(inA) != 1 || inA[0].ID != "wa" {
		t.Errorf("ListWorkflows(pa) = %v, want [wa]", inA)
	}
	if ps, _ := s.ListProjects(); len(ps) < 2 {
		t.Errorf("ListProjects = %d, want >=2", len(ps))
	}
	if _, ok, _ := s.GetProject("pa"); !ok {
		t.Error("GetProject(pa) not found")
	}

	// project members
	if err := s.AddProjectMember(model.ProjectMember{ProjectID: "pa", UserID: "ua", Role: model.RoleOwner, CreatedAt: time.Now()}); err != nil {
		t.Fatalf("AddProjectMember: %v", err)
	}
	if m, ok, err := s.GetProjectMember("pa", "ua"); err != nil || !ok || m.Role != model.RoleOwner {
		t.Fatalf("GetProjectMember: ok=%v err=%v role=%s", ok, err, m.Role)
	}
	if ms, _ := s.ListProjectMembers("pa"); len(ms) != 1 {
		t.Errorf("ListProjectMembers(pa) = %d, want 1", len(ms))
	}
	if err := s.RemoveProjectMember("pa", "ua"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.GetProjectMember("pa", "ua"); ok {
		t.Error("member still present after remove")
	}

	// project-scoped secrets isolate by project
	_ = s.PutSecret("pa", "tk", []byte("A"))
	_ = s.PutSecret("pb", "tk", []byte("B"))
	if ct, _, _ := s.GetSecret("pa", "tk"); string(ct) != "A" {
		t.Errorf("secret pa/tk = %q, want A", ct)
	}
	if names, _ := s.ListSecretNames("pa"); len(names) != 1 || names[0] != "tk" {
		t.Errorf("ListSecretNames(pa) = %v, want [tk]", names)
	}

	// runs: create / get / mutate
	run := &model.Run{
		ID: "r1", WorkflowID: "wf1", Title: "run one",
		Status: model.StatusRunning, CreatedAt: time.Now(),
		Steps:  map[string]*model.StepRun{"a": {NodeID: "a", Status: model.StatusPending}},
		Memory: map[string]any{},
	}
	if _, err := s.CreateRun(run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	snap, ok, err := s.MutateRun("r1", func(r *model.Run) {
		r.Steps["a"].Status = model.StatusSuccess
		r.Memory["k"] = "v"
	})
	if err != nil || !ok {
		t.Fatalf("MutateRun: ok=%v err=%v", ok, err)
	}
	if snap.Steps["a"].Status != model.StatusSuccess {
		t.Errorf("mutate snapshot step = %s, want success", snap.Steps["a"].Status)
	}

	// the mutation must be durable (visible on a fresh read)
	reread, ok, err := s.GetRun("r1")
	if err != nil || !ok {
		t.Fatalf("GetRun: ok=%v err=%v", ok, err)
	}
	if reread.Steps["a"].Status != model.StatusSuccess || reread.Memory["k"] != "v" {
		t.Errorf("mutation not persisted: %+v mem=%v", reread.Steps["a"], reread.Memory)
	}

	// snapshots must be independent copies
	reread.Steps["a"].Status = model.StatusFailed
	again, _, _ := s.GetRun("r1")
	if again.Steps["a"].Status != model.StatusSuccess {
		t.Error("snapshot is not an independent copy — mutating it changed the store")
	}

	if _, ok, _ := s.MutateRun("nope", func(*model.Run) {}); ok {
		t.Error("MutateRun(missing) returned ok=true")
	}

	runs, err := s.ListRuns("")
	if err != nil || len(runs) == 0 {
		t.Fatalf("ListRuns: n=%d err=%v", len(runs), err)
	}

	// secrets: opaque ciphertext round-trips; list and delete work.
	if err := s.PutSecret("default", "tok", []byte{1, 2, 3, 4}); err != nil {
		t.Fatalf("PutSecret: %v", err)
	}
	ct, ok, err := s.GetSecret("default", "tok")
	if err != nil || !ok || len(ct) != 4 || ct[0] != 1 {
		t.Fatalf("GetSecret round-trip failed: ok=%v err=%v ct=%v", ok, err, ct)
	}
	if _, ok, _ := s.GetSecret("default", "missing"); ok {
		t.Error("GetSecret(missing) returned ok=true")
	}
	names, err := s.ListSecretNames("default")
	if err != nil || len(names) == 0 {
		t.Fatalf("ListSecretNames: %v err=%v", names, err)
	}
	if err := s.DeleteSecret("default", "tok"); err != nil {
		t.Fatalf("DeleteSecret: %v", err)
	}
	if _, ok, _ := s.GetSecret("default", "tok"); ok {
		t.Error("secret still present after delete")
	}

	// users
	u := model.User{ID: "u1", Email: "a@b.com", Name: "A", Role: model.RoleAdmin, PasswordHash: "h", CreatedAt: time.Now()}
	if err := s.CreateUser(u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if got, ok, err := s.GetUserByEmail("a@b.com"); err != nil || !ok || got.Role != model.RoleAdmin {
		t.Fatalf("GetUserByEmail: ok=%v err=%v role=%s", ok, err, got.Role)
	}
	if got, ok, err := s.GetUserByID("u1"); err != nil || !ok || got.Email != "a@b.com" {
		t.Fatalf("GetUserByID: ok=%v err=%v", ok, err)
	}
	if n, err := s.CountUsers(); err != nil || n != 1 {
		t.Fatalf("CountUsers = %d err=%v, want 1", n, err)
	}

	// api tokens
	tk := model.APIToken{ID: "t1", Name: "ci", Hash: "deadbeef", Role: model.RoleMember, CreatedAt: time.Now()}
	if err := s.CreateAPIToken(tk); err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	if got, ok, err := s.GetAPITokenByHash("deadbeef"); err != nil || !ok || got.ID != "t1" {
		t.Fatalf("GetAPITokenByHash: ok=%v err=%v", ok, err)
	}
	toks, err := s.ListAPITokens()
	if err != nil || len(toks) != 1 {
		t.Fatalf("ListAPITokens = %d err=%v", len(toks), err)
	}
	if err := s.DeleteAPIToken("t1"); err != nil {
		t.Fatalf("DeleteAPIToken: %v", err)
	}
	if _, ok, _ := s.GetAPITokenByHash("deadbeef"); ok {
		t.Error("token still present after delete")
	}

	// audit log
	if err := s.AppendAudit(model.AuditEntry{ID: "a1", Time: time.Now(), Actor: "x@y.com", Method: "POST", Path: "/api/runs", Status: 201}); err != nil {
		t.Fatalf("AppendAudit: %v", err)
	}
	entries, err := s.ListAudit(10)
	if err != nil || len(entries) == 0 || entries[0].Actor != "x@y.com" {
		t.Fatalf("ListAudit: n=%d err=%v", len(entries), err)
	}
}

func TestMemStoreContract(t *testing.T) {
	storeContract(t, NewMem())
}

func TestPostgresStoreContract(t *testing.T) {
	dsn := os.Getenv("CONDUIT_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("set CONDUIT_TEST_POSTGRES to a DSN to run Postgres store tests")
	}
	pg, err := NewPostgres(dsn)
	if err != nil {
		t.Fatalf("NewPostgres: %v", err)
	}
	defer pg.Close()
	// Start clean so repeated runs are deterministic.
	if _, err := pg.db.Exec(`TRUNCATE workflows, runs, secrets, users, api_tokens, projects, project_members, audit`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	storeContract(t, pg)
}

func TestMemListRunsNewestFirst(t *testing.T) {
	s := NewMem()
	for _, id := range []string{"a", "b", "c"} {
		if _, err := s.CreateRun(&model.Run{ID: id, CreatedAt: time.Now(), Steps: map[string]*model.StepRun{}}); err != nil {
			t.Fatal(err)
		}
	}
	runs, _ := s.ListRuns("")
	if len(runs) != 3 || runs[0].ID != "c" || runs[2].ID != "a" {
		t.Errorf("ListRuns order = %v, want [c b a]", ids(runs))
	}
}

func ids(runs []model.Run) []string {
	out := make([]string, len(runs))
	for i, r := range runs {
		out[i] = r.ID
	}
	return out
}
