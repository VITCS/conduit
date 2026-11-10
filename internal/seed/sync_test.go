package seed

import (
	"testing"

	"conduit/internal/model"
)

// memStore is a tiny in-memory workflowStore for exercising SyncBuiltins.
type memStore struct {
	wf   map[string]model.Workflow
	puts int
}

func newMemStore() *memStore { return &memStore{wf: map[string]model.Workflow{}} }

func (m *memStore) GetWorkflow(id string) (model.Workflow, bool, error) {
	w, ok := m.wf[id]
	return w, ok, nil
}
func (m *memStore) PutWorkflow(w model.Workflow) error {
	m.wf[w.ID] = w
	m.puts++
	return nil
}

func TestSyncInstallsAndStampsFreshWorkflows(t *testing.T) {
	st := newMemStore()
	if err := SyncBuiltins(st, nil); err != nil {
		t.Fatalf("sync: %v", err)
	}
	got, ok, _ := st.GetWorkflow("wf_factory")
	if !ok {
		t.Fatal("wf_factory not installed")
	}
	if got.Rev != 2 {
		t.Errorf("rev = %d, want 2", got.Rev)
	}
	if got.SeedHash == "" || got.SeedHash != Fingerprint(got) {
		t.Errorf("seed hash not stamped correctly: %q", got.SeedHash)
	}
	if !hasNodeType(got, "coding_apply") || !hasNodeType(got, "github_pr") {
		t.Error("real factory workflow should have coding_apply and github_pr nodes")
	}
}

func TestSyncIsIdempotent(t *testing.T) {
	st := newMemStore()
	if err := SyncBuiltins(st, nil); err != nil {
		t.Fatalf("sync 1: %v", err)
	}
	first := st.puts
	if err := SyncBuiltins(st, nil); err != nil {
		t.Fatalf("sync 2: %v", err)
	}
	if st.puts != first {
		t.Errorf("second sync wrote %d extra workflows, want 0", st.puts-first)
	}
}

// A legacy, simulated AI Factory (no coding_apply node, no SeedHash) is the exact
// situation this migration exists for: it must be refreshed to the real pipeline.
func TestSyncRefreshesLegacySimulatedFactory(t *testing.T) {
	st := newMemStore()
	legacy := model.Workflow{
		ID: "wf_factory", ProjectID: FactoryProjectID,
		Name: "AI Factory · Issue → Reviewed PR",
		Nodes: []model.Node{
			{ID: "trig", Type: "github", Kind: model.KindTrigger, Title: "On Issue Labeled"},
			{ID: "plan", Type: "agent", Kind: model.KindStep, Title: "Refine & Plan"},
			{ID: "impl", Type: "agent", Kind: model.KindStep, Title: "Implement (coding agent)"},
			{ID: "pr", Type: "http", Kind: model.KindStep, Title: "Open Pull Request"},
			{ID: "mem", Type: "memory", Kind: model.KindStep, Title: "Record Delivery"},
		},
	}
	_ = st.PutWorkflow(legacy)

	if err := SyncBuiltins(st, nil); err != nil {
		t.Fatalf("sync: %v", err)
	}
	got, _, _ := st.GetWorkflow("wf_factory")
	if got.Name != "AI Factory · Ticket → PR" {
		t.Errorf("legacy factory not refreshed: name = %q", got.Name)
	}
	if !hasNodeType(got, "coding_apply") {
		t.Error("refreshed factory should push code via coding_apply")
	}
	if got.Rev != 2 || got.SeedHash == "" {
		t.Errorf("refreshed factory not stamped: rev=%d hash=%q", got.Rev, got.SeedHash)
	}
}

// A workflow the user has customized (even one sharing the built-in ID) must be
// left exactly as-is — the migration never clobbers edits.
func TestSyncLeavesUserEditedWorkflowAlone(t *testing.T) {
	st := newMemStore()
	// Start from the real factory, then simulate a user edit (add a node), which
	// changes the content but keeps coding_apply present (still "real" shaped).
	edited := FactoryWorkflow()
	edited.Nodes = append(edited.Nodes, model.Node{ID: "custom", Type: "http", Kind: model.KindStep, Title: "My Extra Step"})
	_ = st.PutWorkflow(edited) // no SeedHash (as if saved via the editor)

	if err := SyncBuiltins(st, nil); err != nil {
		t.Fatalf("sync: %v", err)
	}
	got, _, _ := st.GetWorkflow("wf_factory")
	hasCustom := false
	for _, n := range got.Nodes {
		if n.ID == "custom" {
			hasCustom = true
		}
	}
	if !hasCustom {
		t.Error("user edit was overwritten; migration must never clobber edits")
	}
}

// Once stamped, shipping a newer Rev upgrades a still-unedited built-in, but an
// edited one is skipped.
func TestSyncUpgradesStampedUneditedByRev(t *testing.T) {
	st := newMemStore()
	// Seed an "old" stamped built-in: a prior definition (different content, so its
	// fingerprint differs from what we now ship) that was stamped Rev 1 and never
	// edited since (current fingerprint still equals its stored SeedHash).
	old := FactoryWorkflow()
	old.Name = "AI Factory · Ticket → PR (v1)"
	old.Rev = 1
	old.SeedHash = Fingerprint(old)
	_ = st.PutWorkflow(old)
	if Fingerprint(old) == Fingerprint(FactoryWorkflow()) {
		t.Fatal("test setup: old content should differ from current")
	}

	if err := SyncBuiltins(st, nil); err != nil {
		t.Fatalf("sync: %v", err)
	}
	got, _, _ := st.GetWorkflow("wf_factory")
	if got.Rev != 2 {
		t.Errorf("stamped unedited built-in not upgraded: rev = %d, want 2", got.Rev)
	}
}
