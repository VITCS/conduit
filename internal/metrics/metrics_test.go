package metrics

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestObserveAndExposition(t *testing.T) {
	ObserveHTTP("GET", "2xx", 0.01)
	ObserveHTTP("POST", "4xx", 0.02)
	RunsStarted.Inc("")
	RunsActive.Add(2)
	RunsFinished.Inc(Labels("status", "success"))

	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()

	for _, want := range []string{
		"conduit_http_requests_total",
		`method="GET",status="2xx"`,
		"conduit_runs_started_total",
		`conduit_runs_finished_total{status="success"}`,
		"conduit_runs_active",
		"# TYPE conduit_runs_active gauge",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output missing %q\n---\n%s", want, body)
		}
	}
}
