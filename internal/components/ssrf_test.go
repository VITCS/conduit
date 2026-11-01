package components

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"conduit/internal/model"
)

func TestSSRFBlocksLoopbackWhenDisallowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	// Turn the guard ON for this test (TestMain allows private egress globally).
	AllowPrivateEgress = false
	defer func() { AllowPrivateEgress = true }()

	res := For("http")(node("http",
		model.Field{Key: "live", Value: "true"},
		model.Field{Key: "url", Value: srv.URL}, // 127.0.0.1 -> blocked
	), &model.Run{})
	if res.Outcome != model.StatusFailed {
		t.Fatalf("expected SSRF guard to block loopback, got %s", res.Outcome)
	}

	// With the guard off, the same request succeeds.
	AllowPrivateEgress = true
	ok := For("http")(node("http",
		model.Field{Key: "live", Value: "true"},
		model.Field{Key: "url", Value: srv.URL},
	), &model.Run{})
	if ok.Outcome != model.StatusSuccess {
		t.Fatalf("expected success when egress allowed, got %s", ok.Outcome)
	}
}
