// Package components is the registry of component executors. Each component type
// (http, ssh, timegate, …) has an executor that, given a node, produces an
// outcome, a structured output payload, and log lines.
//
// Executors here simulate real side effects deterministically so the engine and
// UI can be exercised end-to-end without external systems. Approval is handled
// specially by the engine (it blocks on a human decision), not here.
package components

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"syscall"
	"time"

	"conduit/internal/model"
)

// AllowPrivateEgress, when true, disables the SSRF guard so outbound executors
// may reach private/loopback addresses. Keep false in production; set it only for
// local testing or a trusted internal network.
var AllowPrivateEgress bool

// controlSSRF runs after DNS resolution with the concrete IP, so it blocks
// requests to private/loopback/link-local ranges even under DNS rebinding.
func controlSSRF(_, address string, _ syscall.RawConn) error {
	if AllowPrivateEgress {
		return nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("blocked egress: cannot parse address %q", address)
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return fmt.Errorf("blocked egress to non-public address %s", ip)
	}
	return nil
}

// liveHTTPClient performs real outbound requests for http/slack/github nodes,
// with an SSRF guard at dial time.
var liveHTTPClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second, Control: controlSSRF}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConns:        50,
	},
}

// llmHTTPClient is used for LLM provider calls (agent / github_review), which
// routinely take far longer than a normal API request to return the first byte.
// Same SSRF guard, but a generous overall timeout.
var llmHTTPClient = &http.Client{
	Timeout: 120 * time.Second,
	Transport: &http.Transport{
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second, Control: controlSSRF}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConns:        20,
	},
}

// ResolveSecret, when set by the host, resolves ${secret:NAME} references found
// in a node's config values, within a project. Nil means no secrets are available.
var ResolveSecret func(projectID, name string) (string, bool)

// ResolveVar resolves ${var:NAME} references to project-level config variables
// (non-secret, plain values set once per project to avoid per-node repetition).
var ResolveVar func(projectID, name string) (string, bool)

var secretRefRe = regexp.MustCompile(`\$\{secret:([A-Za-z0-9_.\-]+)\}`)
var varRefRe = regexp.MustCompile(`\$\{var:([A-Za-z0-9_.\-]+)\}`)

// secretExpand replaces ${secret:NAME} and ${var:NAME} tokens, scoped to the run's
// project. Unknown references are left verbatim so failures are visible.
func secretExpand(projectID, s string) string {
	if strings.Contains(s, "${var:") && ResolveVar != nil {
		s = varRefRe.ReplaceAllStringFunc(s, func(tok string) string {
			name := varRefRe.FindStringSubmatch(tok)[1]
			if v, ok := ResolveVar(projectID, name); ok {
				return v
			}
			return tok
		})
	}
	if ResolveSecret == nil || !strings.Contains(s, "${secret:") {
		return s
	}
	return secretRefRe.ReplaceAllStringFunc(s, func(tok string) string {
		name := secretRefRe.FindStringSubmatch(tok)[1]
		if v, ok := ResolveSecret(projectID, name); ok {
			return v
		}
		return tok
	})
}

// Result is what an executor returns to the engine.
type Result struct {
	Outcome model.Status   // success | failed | created | triggered | skipped
	Output  map[string]any // structured payload, shown in the inspector
	Logs    []string
	// MemoryWrites are applied to the run's shared memory by the engine AFTER the
	// executor returns. Executors return writes here instead of mutating the run
	// directly, so they never run while the store lock is held (a connector doing
	// network I/O must not block the store).
	MemoryWrites map[string]any
}

// Executor runs one node. run is a read-only snapshot (for Input/Memory reads);
// to persist state, return MemoryWrites rather than mutating run.
type Executor func(n model.Node, run *model.Run) Result

var registry = map[string]Executor{
	"github":         execTrigger,
	"webhook":        execTrigger,
	"schedule":       execTrigger,
	"start":          execTrigger,
	"timegate":       execTimegate,
	"http":           execHTTP,
	"graphql":        execGraphQL,
	"slack":          execSlack,
	"github_pr":      execGitHubPR,
	"github_comment": execGitHubComment,
	"github_review":  execGitHubReview,
	"github_merge":   execGitHubMerge,
	"security_scan":  execSecurityScan,
	"jira":           execJira,
	"notion":         execNotion,
	"agent":          execAgent,
	"ssh":            execSSH,
	"if":             execIf,
	"filter":         execFilter,
	"merge":          execMerge,
	"wait":           execWait,
	"memory":         execMemory,
	"status":         execStatus,
	"noop":           execNoop,
}

// engineHandled are valid component types the engine executes directly rather
// than through an executor in the registry (e.g. approval blocks for a decision).
var engineHandled = map[string]bool{"approval": true}

// For returns the executor for a type, falling back to noop.
func For(typ string) Executor {
	if e, ok := registry[typ]; ok {
		return e
	}
	return execNoop
}

// Known reports whether a component type is valid: it has a registered executor
// or is handled directly by the engine.
func Known(typ string) bool {
	if _, ok := registry[typ]; ok {
		return true
	}
	return engineHandled[typ]
}

// Types lists all valid component types (registry + engine-handled).
func Types() []string {
	out := make([]string, 0, len(registry)+len(engineHandled))
	for k := range registry {
		out = append(out, k)
	}
	for k := range engineHandled {
		out = append(out, k)
	}
	return out
}

func execTrigger(n model.Node, run *model.Run) Result {
	out := map[string]any{
		"event":  n.Field("on"),
		"repo":   n.Field("repo"),
		"source": n.Field("url"),
	}
	// Expose the payload the run was started with (e.g. a webhook body).
	if run != nil && len(run.Input) > 0 {
		out["input"] = run.Input
	}
	return Result{
		Outcome: model.StatusTriggered,
		Output:  out,
		Logs:    []string{fmt.Sprintf("trigger fired: %s", firstNonEmpty(n.Field("on"), n.Field("repo"), "event"))},
	}
}

func execTimegate(n model.Node, _ *model.Run) Result {
	return Result{
		Outcome: model.StatusSuccess,
		Output: map[string]any{
			"window":        n.Field("window"),
			"timezone":      n.Field("tz"),
			"within_window": true,
			"held_for":      "0s",
		},
		Logs: []string{fmt.Sprintf("inside window %q (%s) — passing through", n.Field("window"), n.Field("tz"))},
	}
}

func execHTTP(n model.Node, run *model.Run) Result {
	url := firstNonEmpty(n.Field("url"), n.Field("target"))

	// Opt-in real request. The default stays simulated so the seeded demo runs
	// offline and deterministically.
	if strings.EqualFold(n.Field("live"), "true") {
		return liveHTTP(n, url, runProject(run))
	}

	// A health-check-style node fails loudly if its URL names a failure; this
	// keeps the demo deterministic while still exercising the failed branch.
	if strings.Contains(strings.ToLower(url), "fail") {
		return Result{
			Outcome: model.StatusFailed,
			Output:  map[string]any{"status_code": 503, "url": url},
			Logs:    []string{fmt.Sprintf("GET %s -> 503", url)},
		}
	}
	out := map[string]any{
		"status_code": 200,
		"latency_ms":  84,
		"url":         url,
	}
	// Give "Provision Server"-style nodes a richer, resource-shaped payload.
	if region := n.Field("region"); region != "" {
		out = map[string]any{
			"droplet_id": 579011582,
			"name":       "storejs-pr-154",
			"region":     region,
			"size":       n.Field("size"),
			"image":      n.Field("image"),
			"ip":         "159.89.230.13",
			"status":     "active",
		}
	}
	return Result{
		Outcome: model.StatusSuccess,
		Output:  out,
		Logs:    []string{fmt.Sprintf("%s -> ok", firstNonEmpty(url, "request"))},
	}
}

// runProject returns the run's project id (for secret scoping), defaulting safely.
func runProject(run *model.Run) string {
	if run != nil && run.ProjectID != "" {
		return run.ProjectID
	}
	return model.DefaultProjectID
}

// liveHTTP performs a real HTTP request. Method defaults to GET; a body sends as
// the request body. Config values may reference ${secret:NAME} (project-scoped).
// The outcome is success for 2xx, failed otherwise or on error.
func liveHTTP(n model.Node, url, project string) Result {
	url = secretExpand(project, url)
	method := strings.ToUpper(firstNonEmpty(n.Field("method"), "GET"))
	var body io.Reader
	if b := secretExpand(project, n.Field("body")); b != "" {
		body = strings.NewReader(b)
	}
	if url == "" {
		return Result{Outcome: model.StatusFailed, Output: map[string]any{"error": "no url"}, Logs: []string{"no url configured"}}
	}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return Result{Outcome: model.StatusFailed, Output: map[string]any{"error": err.Error()}, Logs: []string{err.Error()}}
	}
	if ct := n.Field("content_type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	if auth := secretExpand(project, n.Field("authorization")); auth != "" {
		req.Header.Set("Authorization", auth)
	}
	start := time.Now()
	resp, err := liveHTTPClient.Do(req)
	if err != nil {
		return Result{Outcome: model.StatusFailed, Output: map[string]any{"url": url, "error": err.Error()}, Logs: []string{fmt.Sprintf("%s %s -> error: %v", method, url, err)}}
	}
	defer resp.Body.Close()
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	outcome := model.StatusSuccess
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		outcome = model.StatusFailed
	}
	return Result{
		Outcome: outcome,
		Output: map[string]any{
			"url":         url,
			"method":      method,
			"status_code": resp.StatusCode,
			"latency_ms":  time.Since(start).Milliseconds(),
			"body":        string(snippet),
		},
		Logs: []string{fmt.Sprintf("%s %s -> %d (%dms)", method, url, resp.StatusCode, time.Since(start).Milliseconds())},
	}
}

func execSSH(n model.Node, _ *model.Run) Result {
	return Result{
		Outcome: model.StatusSuccess,
		Output: map[string]any{
			"host":      n.Field("host"),
			"command":   n.Field("run"),
			"exit_code": 0,
			"duration":  "47s",
		},
		Logs: []string{
			fmt.Sprintf("ssh %s", n.Field("host")),
			fmt.Sprintf("run %s -> exit 0", n.Field("run")),
		},
	}
}

func execIf(n model.Node, _ *model.Run) Result {
	truthy := !strings.EqualFold(strings.TrimSpace(n.Field("result")), "false")
	outcome := model.StatusSuccess
	if !truthy {
		outcome = model.StatusFailed
	}
	return Result{
		Outcome: outcome,
		Output:  map[string]any{"condition": n.Field("expr"), "result": truthy},
		Logs:    []string{fmt.Sprintf("if (%s) => %v", n.Field("expr"), truthy)},
	}
}

// execFilter passes the branch through when its condition holds, otherwise returns
// a skipped outcome that stops the branch WITHOUT failing the run. Conditions
// (most specific first), with `value` templated from ${input.*}/${memory.*}/secrets:
//   - value + in="a,b,c"      → pass if value is one of the list (case-insensitive)
//   - value + equals=X        → pass if value == X
//   - value + not_equals=X    → pass if value != X
//   - result="false"          → stop (literal fallback, back-compat)
func execFilter(n model.Node, run *model.Run) Result {
	project := runProject(run)
	value := expandTemplate(run, secretExpand(project, n.Field("value")))
	// An unresolved reference (e.g. ${input.branch} when the payload has no branch)
	// stays verbatim; treat that as "absent" so presence/equality checks behave.
	if strings.Contains(value, "${") {
		value = ""
	}
	pass := true
	reason := n.Field("expr")

	switch {
	case strings.EqualFold(n.Field("not_empty"), "true"):
		reason = fmt.Sprintf("%q is not empty", value)
		pass = strings.TrimSpace(value) != ""
	case n.Field("in") != "":
		reason = fmt.Sprintf("%q in [%s]", value, n.Field("in"))
		pass = false
		for _, opt := range strings.Split(n.Field("in"), ",") {
			if strings.EqualFold(strings.TrimSpace(opt), strings.TrimSpace(value)) {
				pass = true
				break
			}
		}
	case n.Field("equals") != "":
		reason = fmt.Sprintf("%q == %q", value, n.Field("equals"))
		pass = strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(expandTemplate(run, secretExpand(project, n.Field("equals")))))
	case n.Field("not_equals") != "":
		reason = fmt.Sprintf("%q != %q", value, n.Field("not_equals"))
		pass = !strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(expandTemplate(run, secretExpand(project, n.Field("not_equals")))))
	default:
		pass = !strings.EqualFold(strings.TrimSpace(n.Field("result")), "false")
	}

	if pass {
		return Result{Outcome: model.StatusSuccess,
			Output: map[string]any{"condition": reason, "value": value, "passed": true},
			Logs:   []string{fmt.Sprintf("filter %s => pass", reason)}}
	}
	return Result{Outcome: model.StatusSkipped,
		Output: map[string]any{"condition": reason, "value": value, "passed": false},
		Logs:   []string{fmt.Sprintf("filter %s => stop", reason)}}
}

// execMerge is a pass-through join point. True join semantics (wait for all
// predecessors) are a later addition; today it succeeds so the branch continues.
func execMerge(_ model.Node, _ *model.Run) Result {
	return Result{Outcome: model.StatusSuccess, Output: map[string]any{"merged": true}, Logs: []string{"merged"}}
}

// execGraphQL simulates a GraphQL call (mirrors the simulated http default).
func execGraphQL(n model.Node, _ *model.Run) Result {
	return Result{
		Outcome: model.StatusSuccess,
		Output: map[string]any{
			"endpoint": n.Field("url"),
			"query":    n.Field("query"),
			"data":     map[string]any{"ok": true},
		},
		Logs: []string{fmt.Sprintf("graphql %s -> ok", firstNonEmpty(n.Field("url"), "endpoint"))},
	}
}

// execSlack posts a message to a Slack-style incoming webhook. The webhook URL is
// typically a secret reference (webhook: "${secret:slack_url}").
func execSlack(n model.Node, run *model.Run) Result {
	project := runProject(run)
	webhook := secretExpand(project, firstNonEmpty(n.Field("webhook"), n.Field("url")))
	text := secretExpand(project, firstNonEmpty(n.Field("text"), n.Field("message")))
	if webhook == "" {
		return Result{Outcome: model.StatusFailed, Output: map[string]any{"error": "no webhook"}, Logs: []string{"no slack webhook configured"}}
	}
	payload := fmt.Sprintf(`{"text":%q}`, text)
	req, err := http.NewRequest(http.MethodPost, webhook, strings.NewReader(payload))
	if err != nil {
		return Result{Outcome: model.StatusFailed, Output: map[string]any{"error": err.Error()}, Logs: []string{err.Error()}}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := liveHTTPClient.Do(req)
	if err != nil {
		return Result{Outcome: model.StatusFailed, Output: map[string]any{"error": err.Error()}, Logs: []string{fmt.Sprintf("slack post failed: %v", err)}}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	outcome := model.StatusSuccess
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		outcome = model.StatusFailed
	}
	return Result{
		Outcome: outcome,
		Output:  map[string]any{"status_code": resp.StatusCode, "text": text},
		Logs:    []string{fmt.Sprintf("slack notify -> %d", resp.StatusCode)},
	}
}

func execWait(n model.Node, _ *model.Run) Result {
	return Result{
		Outcome: model.StatusSuccess,
		Output:  map[string]any{"waited": firstNonEmpty(n.Field("duration"), "1s")},
		Logs:    []string{fmt.Sprintf("waited %s", firstNonEmpty(n.Field("duration"), "1s"))},
	}
}

func execMemory(n model.Node, _ *model.Run) Result {
	store := firstNonEmpty(n.Field("store"), "default")
	key := firstNonEmpty(n.Field("key"), "pr_number")
	value := map[string]any{
		"droplet_id": 579011582,
		"url":        "pr-154.preview.storejs.dev",
		"sha":        "f62f1a9",
	}
	memKey := fmt.Sprintf("%s/%s", store, "154")
	return Result{
		Outcome:      model.StatusSuccess,
		Output:       map[string]any{"store": store, "key": key, "value": value},
		Logs:         []string{fmt.Sprintf("wrote %s", memKey)},
		MemoryWrites: map[string]any{memKey: value},
	}
}

func execStatus(n model.Node, run *model.Run) Result {
	state := firstNonEmpty(n.Field("set"), "success")
	return Result{
		Outcome: model.StatusCreated,
		Output:  map[string]any{"state": state, "notify": n.Field("notify")},
		Logs:    []string{fmt.Sprintf("status set: %s", state)},
	}
}

func execNoop(_ model.Node, _ *model.Run) Result {
	return Result{Outcome: model.StatusSuccess, Output: map[string]any{}, Logs: []string{"noop"}}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
