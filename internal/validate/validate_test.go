package validate

import (
	"strings"
	"testing"

	"conduit/internal/model"
	"conduit/internal/seed"
)

func node(id, kind, typ string) model.Node {
	return model.Node{ID: id, Kind: kind, Type: typ, Title: id}
}

func TestSeedWorkflowIsValid(t *testing.T) {
	if errs := Workflow(seed.Workflow()); len(errs) != 0 {
		t.Fatalf("seed workflow should be valid, got: %v", errs)
	}
}

func TestFactoryWorkflowIsValid(t *testing.T) {
	if errs := Workflow(seed.FactoryWorkflow()); len(errs) != 0 {
		t.Fatalf("factory workflow should be valid, got: %v", errs)
	}
}

func TestRetryBackEdgeAccepted(t *testing.T) {
	// a → b → c, with a bounded retry back-edge c→a on failure.
	wf := model.Workflow{
		ID: "w", ProjectID: "default", Name: "retry",
		Nodes: []model.Node{
			node("a", model.KindStep, "noop"),
			node("b", model.KindStep, "noop"),
			node("c", model.KindStep, "noop"),
		},
		Edges: []model.Edge{
			{From: "a", To: "b", Label: "success"},
			{From: "b", To: "c", Label: "success"},
			{From: "c", To: "a", Label: "failed", MaxAttempts: 2},
		},
	}
	if errs := Workflow(wf); len(errs) != 0 {
		t.Fatalf("retry back-edge should be valid, got: %v", errs)
	}
}

func TestRetryEdgeRejections(t *testing.T) {
	base := func(e model.Edge) model.Workflow {
		return model.Workflow{ID: "w", ProjectID: "default", Name: "r",
			Nodes: []model.Node{node("a", model.KindStep, "noop"), node("b", model.KindStep, "noop")},
			Edges: []model.Edge{{From: "a", To: "b", Label: "success"}, e}}
	}
	// retry edge must be a real back-edge (b cannot reach a here)
	if errs := Workflow(base(model.Edge{From: "a", To: "b", Label: "failed", MaxAttempts: 2})); !hasErr(errs, "not a back-edge") {
		t.Errorf("expected back-edge error, got %v", errs)
	}
	// wrong label
	if errs := Workflow(base(model.Edge{From: "b", To: "a", Label: "success", MaxAttempts: 2})); !hasErr(errs, `must have label "failed"`) {
		t.Errorf("expected label error, got %v", errs)
	}
	// out-of-range budget
	if errs := Workflow(base(model.Edge{From: "b", To: "a", Label: "failed", MaxAttempts: 99})); !hasErr(errs, "max_attempts") {
		t.Errorf("expected max_attempts error, got %v", errs)
	}
}

func TestPlainCycleStillRejected(t *testing.T) {
	// A non-retry cycle is still an error.
	wf := model.Workflow{ID: "w", ProjectID: "default", Name: "c",
		Nodes: []model.Node{node("a", model.KindStep, "noop"), node("b", model.KindStep, "noop")},
		Edges: []model.Edge{{From: "a", To: "b", Label: "success"}, {From: "b", To: "a", Label: "success"}}}
	if errs := Workflow(wf); !hasErr(errs, "cycle") {
		t.Errorf("expected cycle error, got %v", errs)
	}
}

func hasErr(errs []string, substr string) bool {
	for _, e := range errs {
		if strings.Contains(e, substr) {
			return true
		}
	}
	return false
}

func TestDanglingEdge(t *testing.T) {
	wf := model.Workflow{
		Name:  "w",
		Nodes: []model.Node{node("a", model.KindTrigger, "github")},
		Edges: []model.Edge{{From: "a", To: "ghost"}},
	}
	errs := Workflow(wf)
	if !hasErr(errs, `unknown node "ghost"`) {
		t.Errorf("expected dangling-edge error, got %v", errs)
	}
}

func TestCycleRejected(t *testing.T) {
	wf := model.Workflow{
		Name: "w",
		Nodes: []model.Node{
			node("a", model.KindTrigger, "github"),
			node("b", model.KindStep, "http"),
			node("c", model.KindStep, "http"),
		},
		Edges: []model.Edge{{From: "a", To: "b"}, {From: "b", To: "c"}, {From: "c", To: "b"}},
	}
	errs := Workflow(wf)
	if !hasErr(errs, "cycle") {
		t.Errorf("expected cycle error, got %v", errs)
	}
}

func TestNoFalseCycleOnDiamond(t *testing.T) {
	// a -> b, a -> c, b -> d, c -> d  (DAG with a diamond; must be valid)
	wf := model.Workflow{
		Name: "w",
		Nodes: []model.Node{
			node("a", model.KindTrigger, "github"),
			node("b", model.KindStep, "http"),
			node("c", model.KindStep, "http"),
			node("d", model.KindStatus, "status"),
		},
		Edges: []model.Edge{{From: "a", To: "b"}, {From: "a", To: "c"}, {From: "b", To: "d"}, {From: "c", To: "d"}},
	}
	if errs := Workflow(wf); len(errs) != 0 {
		t.Errorf("diamond DAG should be valid, got %v", errs)
	}
}

func TestDuplicateIDAndSelfLoopAndUnknownType(t *testing.T) {
	wf := model.Workflow{
		Name: "w",
		Nodes: []model.Node{
			node("a", model.KindTrigger, "github"),
			node("a", model.KindStep, "http"),        // duplicate id
			node("x", model.KindStep, "nonexistent"), // unknown type
		},
		Edges: []model.Edge{{From: "a", To: "a"}}, // self-loop
	}
	errs := Workflow(wf)
	if !hasErr(errs, "duplicate node id") {
		t.Errorf("expected duplicate id error, got %v", errs)
	}
	if !hasErr(errs, "loops to itself") {
		t.Errorf("expected self-loop error, got %v", errs)
	}
	if !hasErr(errs, "unknown component type") {
		t.Errorf("expected unknown type error, got %v", errs)
	}
}

func TestInvalidLabelAndMissingName(t *testing.T) {
	wf := model.Workflow{
		Nodes: []model.Node{node("a", model.KindTrigger, "github"), node("b", model.KindStep, "http")},
		Edges: []model.Edge{{From: "a", To: "b", Label: "maybe"}},
	}
	errs := Workflow(wf)
	if !hasErr(errs, "name is required") {
		t.Errorf("expected missing-name error, got %v", errs)
	}
	if !hasErr(errs, `invalid label "maybe"`) {
		t.Errorf("expected invalid-label error, got %v", errs)
	}
}
