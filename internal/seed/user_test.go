package seed

import (
	"testing"

	"conduit/internal/model"
)

type memWF map[string]model.Workflow

func (m memWF) GetWorkflow(id string) (model.Workflow, bool, error) {
	w, ok := m[id]
	return w, ok, nil
}
func (m memWF) PutWorkflow(w model.Workflow) error { m[w.ID] = w; return nil }

func TestUserWorkflowsAreRehomedCopies(t *testing.T) {
	key := UserKey("usr_abc")
	if key != UserKey("usr_abc") || key == UserKey("usr_abd") || len(key) != 12 {
		t.Fatalf("UserKey not stable/unique: %q", key)
	}
	wfs := UserWorkflows(key)
	if len(wfs) != 2 {
		t.Fatalf("got %d workflows", len(wfs))
	}
	f, g := wfs[0], wfs[1]
	if f.ID != UserFactoryWorkflowID(key) || f.ProjectID != UserFactoryProjectID(key) {
		t.Errorf("factory copy = %s in %s", f.ID, f.ProjectID)
	}
	if g.ID != UserGitHubWorkflowID(key) || g.ProjectID != UserGitHubProjectID(key) {
		t.Errorf("github copy = %s in %s", g.ID, g.ProjectID)
	}
	// The shipped definitions are not mutated by making a copy.
	if GitHubWorkflow().ID != GitHubWorkflowID || GitHubWorkflow().Nodes[0].Config[1].Value != "POST /api/hooks/wf_github?token=…" {
		t.Error("copying mutated the shipped GitHub workflow")
	}
	if ReviewWorkflowFor(UserFactoryProjectID(key)) != UserGitHubWorkflowID(key) {
		t.Error("user factory must hand off to the user's own review workflow")
	}
	if ReviewWorkflowFor(FactoryProjectID) != GitHubWorkflowID || ReviewWorkflowFor("my-project") != GitHubWorkflowID {
		t.Error("shared projects must hand off to the shared review workflow")
	}
}

func TestSyncUserWorkflowsNeverOverwritesEdits(t *testing.T) {
	st := memWF{}
	key := UserKey("usr_x")
	if err := SyncUserWorkflows(st, key, nil); err != nil {
		t.Fatal(err)
	}
	id := UserFactoryWorkflowID(key)
	w := st[id]
	w.Name = "My edited factory"
	st[id] = w
	if err := SyncUserWorkflows(st, key, nil); err != nil {
		t.Fatal(err)
	}
	if st[id].Name != "My edited factory" {
		t.Fatal("re-sync overwrote the user's edit")
	}
}
