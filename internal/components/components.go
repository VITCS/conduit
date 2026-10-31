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
	"strings"

	"conduit/internal/model"
)

// Result is what an executor returns to the engine.
type Result struct {
	Outcome model.Status   // success | failed | created | triggered
	Output  map[string]any // structured payload, shown in the inspector
	Logs    []string
}

// Executor runs one node. run carries shared run-scoped state (e.g. memory).
type Executor func(n model.Node, run *model.Run) Result

var registry = map[string]Executor{
	"github":   execTrigger,
	"webhook":  execTrigger,
	"timegate": execTimegate,
	"http":     execHTTP,
	"ssh":      execSSH,
	"if":       execIf,
	"wait":     execWait,
	"memory":   execMemory,
	"status":   execStatus,
	"noop":     execNoop,
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

func execTrigger(n model.Node, _ *model.Run) Result {
	return Result{
		Outcome: model.StatusTriggered,
		Output: map[string]any{
			"event":  n.Field("on"),
			"repo":   n.Field("repo"),
			"source": n.Field("url"),
		},
		Logs: []string{fmt.Sprintf("trigger fired: %s", firstNonEmpty(n.Field("on"), n.Field("repo"), "event"))},
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

func execHTTP(n model.Node, _ *model.Run) Result {
	// A health-check-style node fails loudly if its URL names a failure; this
	// keeps the demo deterministic while still exercising the failed branch.
	url := firstNonEmpty(n.Field("url"), n.Field("target"))
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

func execWait(n model.Node, _ *model.Run) Result {
	return Result{
		Outcome: model.StatusSuccess,
		Output:  map[string]any{"waited": firstNonEmpty(n.Field("duration"), "1s")},
		Logs:    []string{fmt.Sprintf("waited %s", firstNonEmpty(n.Field("duration"), "1s"))},
	}
}

func execMemory(n model.Node, run *model.Run) Result {
	store := firstNonEmpty(n.Field("store"), "default")
	key := firstNonEmpty(n.Field("key"), "pr_number")
	value := map[string]any{
		"droplet_id": 579011582,
		"url":        "pr-154.preview.storejs.dev",
		"sha":        "f62f1a9",
	}
	if run.Memory == nil {
		run.Memory = map[string]any{}
	}
	run.Memory[fmt.Sprintf("%s/%s", store, "154")] = value
	return Result{
		Outcome: model.StatusSuccess,
		Output:  map[string]any{"store": store, "key": key, "value": value},
		Logs:    []string{fmt.Sprintf("wrote %s/%s", store, "154")},
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
