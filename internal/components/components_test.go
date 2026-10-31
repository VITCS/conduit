package components

import (
	"testing"

	"conduit/internal/model"
)

func node(typ string, fields ...model.Field) model.Node {
	return model.Node{ID: "n", Type: typ, Config: fields}
}

func TestHTTPSuccessAndFailure(t *testing.T) {
	ok := For("http")(node("http", model.Field{Key: "url", Value: "http://svc/healthz"}), &model.Run{})
	if ok.Outcome != model.StatusSuccess {
		t.Errorf("healthz outcome = %s, want success", ok.Outcome)
	}
	if ok.Output["status_code"] != 200 {
		t.Errorf("status_code = %v, want 200", ok.Output["status_code"])
	}

	bad := For("http")(node("http", model.Field{Key: "url", Value: "http://svc/fail"}), &model.Run{})
	if bad.Outcome != model.StatusFailed {
		t.Errorf("fail url outcome = %s, want failed", bad.Outcome)
	}
}

func TestHTTPResourceShape(t *testing.T) {
	res := For("http")(node("http",
		model.Field{Key: "region", Value: "nyc1"},
		model.Field{Key: "size", Value: "s-1vcpu-1gb"},
	), &model.Run{})
	if res.Output["region"] != "nyc1" {
		t.Errorf("region = %v, want nyc1", res.Output["region"])
	}
	if _, ok := res.Output["droplet_id"]; !ok {
		t.Error("provision-style node missing droplet_id in output")
	}
}

func TestMemoryWritesRunMemory(t *testing.T) {
	run := &model.Run{Memory: map[string]any{}}
	res := For("memory")(node("memory",
		model.Field{Key: "store", Value: "preview-envs"},
		model.Field{Key: "key", Value: "pr_number"},
	), run)
	if res.Outcome != model.StatusSuccess {
		t.Fatalf("memory outcome = %s", res.Outcome)
	}
	if _, ok := run.Memory["preview-envs/154"]; !ok {
		t.Errorf("run memory not written, keys = %v", run.Memory)
	}
}

func TestStatusAndNoop(t *testing.T) {
	s := For("status")(node("status", model.Field{Key: "set", Value: "failure"}), &model.Run{})
	if s.Outcome != model.StatusCreated {
		t.Errorf("status outcome = %s, want created", s.Outcome)
	}
	n := For("does-not-exist")(node("x"), &model.Run{})
	if n.Outcome != model.StatusSuccess {
		t.Errorf("unknown type should fall back to noop success, got %s", n.Outcome)
	}
}

func TestIfBranch(t *testing.T) {
	yes := For("if")(node("if", model.Field{Key: "result", Value: "true"}), &model.Run{})
	if yes.Outcome != model.StatusSuccess {
		t.Errorf("if true -> %s, want success", yes.Outcome)
	}
	no := For("if")(node("if", model.Field{Key: "result", Value: "false"}), &model.Run{})
	if no.Outcome != model.StatusFailed {
		t.Errorf("if false -> %s, want failed", no.Outcome)
	}
}
