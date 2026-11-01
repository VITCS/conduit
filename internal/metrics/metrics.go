// Package metrics is a tiny, dependency-free Prometheus text exposition for the
// handful of series Conduit cares about. It avoids pulling a metrics library so
// the single-binary stays lean; swap in client_golang if richer metrics are
// needed later.
package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
)

// counterVec is a labeled counter.
type counterVec struct {
	mu     sync.Mutex
	name   string
	help   string
	values map[string]float64 // label-string -> value
}

func newCounter(name, help string) *counterVec {
	return &counterVec{name: name, help: help, values: map[string]float64{}}
}

func (c *counterVec) Inc(labels string) { c.Add(labels, 1) }
func (c *counterVec) Add(labels string, v float64) {
	c.mu.Lock()
	c.values[labels] += v
	c.mu.Unlock()
}

// gauge is a single float that goes up and down.
type gauge struct {
	mu   sync.Mutex
	name string
	help string
	v    float64
}

func newGauge(name, help string) *gauge { return &gauge{name: name, help: help} }
func (g *gauge) Add(v float64) {
	g.mu.Lock()
	g.v += v
	g.mu.Unlock()
}

// The metric set.
var (
	HTTPRequests    = newCounter("conduit_http_requests_total", "Total HTTP requests by method and status.")
	HTTPDurationSum = newCounter("conduit_http_request_duration_seconds_sum", "Sum of HTTP request durations.")
	HTTPDurationCnt = newCounter("conduit_http_request_duration_seconds_count", "Count of HTTP requests measured.")
	RunsStarted     = newCounter("conduit_runs_started_total", "Total workflow runs started.")
	RunsFinished    = newCounter("conduit_runs_finished_total", "Total workflow runs finished by status.")
	RunsActive      = newGauge("conduit_runs_active", "Currently executing runs.")
)

// label renders a single label pair as prometheus label syntax.
func label(pairs ...string) string {
	if len(pairs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteByte('{')
	for i := 0; i < len(pairs); i += 2 {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `%s=%q`, pairs[i], pairs[i+1])
	}
	b.WriteByte('}')
	return b.String()
}

// Labels builds a stable label string from key/value pairs.
func Labels(pairs ...string) string { return label(pairs...) }

// ObserveHTTP records one request.
func ObserveHTTP(method, status string, seconds float64) {
	HTTPRequests.Inc(label("method", method, "status", status))
	HTTPDurationSum.Add("", seconds)
	HTTPDurationCnt.Add("", 1)
}

// Handler serves the metrics in Prometheus text format.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		var b strings.Builder
		for _, c := range []*counterVec{HTTPRequests, HTTPDurationSum, HTTPDurationCnt, RunsStarted, RunsFinished} {
			writeCounter(&b, c)
		}
		writeGauge(&b, RunsActive)
		_, _ = w.Write([]byte(b.String()))
	})
}

func writeCounter(b *strings.Builder, c *counterVec) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s counter\n", c.name, c.help, c.name)
	keys := make([]string, 0, len(c.values))
	for k := range c.values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		fmt.Fprintf(b, "%s 0\n", c.name)
		return
	}
	for _, k := range keys {
		fmt.Fprintf(b, "%s%s %g\n", c.name, k, c.values[k])
	}
}

func writeGauge(b *strings.Builder, g *gauge) {
	g.mu.Lock()
	defer g.mu.Unlock()
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s gauge\n%s %g\n", g.name, g.help, g.name, g.name, g.v)
}
