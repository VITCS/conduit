// Package validate checks that a workflow is well-formed before it is stored.
// It rejects the graph problems that would make a run undefined or non-terminating:
// duplicate/empty node IDs, unknown component types, dangling or self-looping
// edges, duplicate edges, invalid edge labels, and cycles (the graph must be a DAG).
package validate

import (
	"fmt"
	"sort"
	"strings"

	"conduit/internal/components"
	"conduit/internal/model"
)

var validKinds = map[string]bool{
	model.KindTrigger: true,
	model.KindStep:    true,
	model.KindStatus:  true,
}

var validLabels = map[string]bool{"": true, "success": true, "failed": true, "approved": true}

// Workflow returns a list of human-readable problems. An empty slice means valid.
func Workflow(wf model.Workflow) []string {
	var errs []string

	if strings.TrimSpace(wf.Name) == "" {
		errs = append(errs, "workflow name is required")
	}

	// Nodes: unique non-empty IDs, valid kind, known component type.
	ids := make(map[string]bool, len(wf.Nodes))
	for i, n := range wf.Nodes {
		if strings.TrimSpace(n.ID) == "" {
			errs = append(errs, fmt.Sprintf("node #%d has an empty id", i+1))
			continue
		}
		if ids[n.ID] {
			errs = append(errs, fmt.Sprintf("duplicate node id %q", n.ID))
			continue
		}
		ids[n.ID] = true
		if !validKinds[n.Kind] {
			errs = append(errs, fmt.Sprintf("node %q has invalid kind %q", n.ID, n.Kind))
		}
		if !components.Known(n.Type) {
			errs = append(errs, fmt.Sprintf("node %q has unknown component type %q", n.ID, n.Type))
		}
	}

	// Edges: endpoints must exist, no self-loops, no duplicates, valid label.
	seen := make(map[string]bool, len(wf.Edges))
	for _, e := range wf.Edges {
		if !ids[e.From] {
			errs = append(errs, fmt.Sprintf("edge from unknown node %q", e.From))
		}
		if !ids[e.To] {
			errs = append(errs, fmt.Sprintf("edge to unknown node %q", e.To))
		}
		if e.From == e.To {
			errs = append(errs, fmt.Sprintf("edge from %q loops to itself", e.From))
		}
		if !validLabels[e.Label] {
			errs = append(errs, fmt.Sprintf("edge %s→%s has invalid label %q", e.From, e.To, e.Label))
		}
		key := e.From + "\x00" + e.To
		if seen[key] {
			errs = append(errs, fmt.Sprintf("duplicate edge %s→%s", e.From, e.To))
		}
		seen[key] = true
	}

	// Cycle check (only over edges whose endpoints both exist, to avoid noise
	// when dangling edges were already reported).
	if cyc := findCycle(ids, wf.Edges); len(cyc) > 0 {
		errs = append(errs, "workflow has a cycle: "+strings.Join(cyc, " → "))
	}

	sort.Strings(errs)
	return errs
}

// findCycle returns one cycle's node sequence, or nil if the graph is acyclic.
// Uses DFS with a recursion stack so it can report the offending path.
func findCycle(ids map[string]bool, edges []model.Edge) []string {
	adj := make(map[string][]string, len(ids))
	for _, e := range edges {
		if ids[e.From] && ids[e.To] && e.From != e.To {
			adj[e.From] = append(adj[e.From], e.To)
		}
	}
	const (
		white = 0 // unvisited
		gray  = 1 // on the current DFS stack
		black = 2 // fully explored
	)
	color := make(map[string]int, len(ids))
	var stack []string

	// Deterministic iteration order for stable error messages.
	order := make([]string, 0, len(ids))
	for id := range ids {
		order = append(order, id)
	}
	sort.Strings(order)

	var dfs func(string) []string
	dfs = func(u string) []string {
		color[u] = gray
		stack = append(stack, u)
		neighbors := append([]string(nil), adj[u]...)
		sort.Strings(neighbors)
		for _, v := range neighbors {
			if color[v] == gray {
				// Found a back-edge; slice the stack from v to close the loop.
				for i, s := range stack {
					if s == v {
						return append(append([]string(nil), stack[i:]...), v)
					}
				}
				return []string{v, v}
			}
			if color[v] == white {
				if c := dfs(v); c != nil {
					return c
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[u] = black
		return nil
	}

	for _, id := range order {
		if color[id] == white {
			if c := dfs(id); c != nil {
				return c
			}
		}
	}
	return nil
}
