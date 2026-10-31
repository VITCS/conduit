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
