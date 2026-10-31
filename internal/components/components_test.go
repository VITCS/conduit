package components

import (
	"io"
	"net/http"
	"net/http/httptest"
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
	res := For("memory")(node("memory",
		model.Field{Key: "store", Value: "preview-envs"},
		model.Field{Key: "key", Value: "pr_number"},
	), &model.Run{})
	if res.Outcome != model.StatusSuccess {
		t.Fatalf("memory outcome = %s", res.Outcome)
	}
	// Writes are returned for the engine to apply, not mutated in place.
	if _, ok := res.MemoryWrites["preview-envs/154"]; !ok {
		t.Errorf("memory writes missing key, got %v", res.MemoryWrites)
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

func TestLiveHTTP(t *testing.T) {
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("pong"))
	}))
	defer okSrv.Close()
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer errSrv.Close()

	ok := For("http")(node("http",
		model.Field{Key: "live", Value: "true"},
		model.Field{Key: "url", Value: okSrv.URL},
	), &model.Run{})
	if ok.Outcome != model.StatusSuccess {
		t.Errorf("live 200 outcome = %s, want success", ok.Outcome)
	}
	if ok.Output["status_code"] != 200 {
		t.Errorf("status_code = %v, want 200", ok.Output["status_code"])
	}
	if ok.Output["body"] != "pong" {
		t.Errorf("body = %v, want pong", ok.Output["body"])
	}

	bad := For("http")(node("http",
		model.Field{Key: "live", Value: "true"},
		model.Field{Key: "url", Value: errSrv.URL},
	), &model.Run{})
	if bad.Outcome != model.StatusFailed {
		t.Errorf("live 500 outcome = %s, want failed", bad.Outcome)
	}
}

func TestSlackPostsToWebhook(t *testing.T) {
	var gotBody string
	var gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		gotCT = r.Header.Get("Content-Type")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	res := For("slack")(node("slack",
		model.Field{Key: "webhook", Value: srv.URL},
		model.Field{Key: "text", Value: "deploy done"},
	), &model.Run{})
	if res.Outcome != model.StatusSuccess {
		t.Fatalf("slack outcome = %s, want success", res.Outcome)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type = %q, want application/json", gotCT)
	}
	if gotBody != `{"text":"deploy done"}` {
		t.Errorf("body = %q", gotBody)
	}

	// no webhook -> failed
	none := For("slack")(node("slack", model.Field{Key: "text", Value: "x"}), &model.Run{})
	if none.Outcome != model.StatusFailed {
		t.Errorf("slack with no webhook = %s, want failed", none.Outcome)
	}
}

func TestSecretExpansionInSlackWebhook(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(200)
	}))
	defer srv.Close()

	// Install a resolver mapping a secret name to the test server URL.
	prev := ResolveSecret
	ResolveSecret = func(name string) (string, bool) {
		if name == "slack_url" {
			return srv.URL, true
		}
		return "", false
	}
	defer func() { ResolveSecret = prev }()

	res := For("slack")(node("slack",
		model.Field{Key: "webhook", Value: "${secret:slack_url}"},
		model.Field{Key: "text", Value: "hi"},
	), &model.Run{})
	if res.Outcome != model.StatusSuccess || !hit {
		t.Errorf("secret-resolved slack: outcome=%s hit=%v", res.Outcome, hit)
	}
}

func TestSecretExpandNoResolverLeavesTokens(t *testing.T) {
	prev := ResolveSecret
	ResolveSecret = nil
	defer func() { ResolveSecret = prev }()
	if got := secretExpand("x=${secret:foo}"); got != "x=${secret:foo}" {
		t.Errorf("expand with no resolver = %q, want unchanged", got)
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
