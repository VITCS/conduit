package components

import (
	"fmt"
	"strings"

	"conduit/internal/model"
)

// Shared guidance every agent prompt can carry:
//
//   - the project context (Settings → Project context, variable project_context):
//     the goal, stack, build/test commands, conventions and what is off-limits;
//   - reviewer feedback when an approval gate sent this step's proposal back
//     (run memory feedback_for_<nodeID>, written by the engine), together with the
//     step's previous proposal so the agent revises instead of starting over.

const maxContextChars = 12000

// projectContext is the project's shared context for agents, or "".
func projectContext(project string) string {
	c := projVar(project, "project_context")
	if len(c) > maxContextChars {
		c = c[:maxContextChars] + "\n…(truncated)"
	}
	return c
}

// contextBlock renders the project context as a prompt preamble, or "".
func contextBlock(project string) string {
	c := projectContext(project)
	if c == "" {
		return ""
	}
	return "PROJECT CONTEXT (written by the team for every agent — follow it):\n" + c + "\n\n"
}

// reviewerFeedback renders the feedback a reviewer gave when sending this node's
// proposal back, plus its previous proposal (the first non-empty step output
// among prevKeys), or "" when the node was not sent back.
func reviewerFeedback(run *model.Run, nodeID string, prevKeys ...string) string {
	if run == nil {
		return ""
	}
	fb, _ := run.Memory["feedback_for_"+nodeID].(string)
	fb = strings.TrimSpace(fb)
	if fb == "" {
		return ""
	}
	var prev string
	if st := run.Steps[nodeID]; st != nil {
		for _, k := range prevKeys {
			if v, ok := st.Output[k].(string); ok && strings.TrimSpace(v) != "" {
				prev = strings.TrimSpace(v)
				break
			}
		}
	}
	b := "\n\nA REVIEWER SENT YOUR PREVIOUS PROPOSAL BACK. Revise it to address this feedback:\n" + fb
	if prev != "" {
		if len(prev) > 4000 {
			prev = prev[:4000] + "\n…"
		}
		b += "\n\nYOUR PREVIOUS PROPOSAL (for reference — change what the feedback asks):\n" + prev
	}
	return b
}

// repoGuidance fetches the repository's own agent guidance files (CLAUDE.md,
// AGENTS.md) at ref, for the direct-API coding engine. Claude Code reads these
// itself. Best effort: missing files or no token yield "".
func (c codingCfg) repoGuidance(ref string) string {
	if !c.hasToken {
		return ""
	}
	var b strings.Builder
	for _, p := range []string{"CLAUDE.md", "AGENTS.md"} {
		content, ok := c.gh.fileContent(p, ref)
		if !ok || strings.TrimSpace(content) == "" {
			continue
		}
		if len(content) > 6000 {
			content = content[:6000] + "\n…(truncated)"
		}
		fmt.Fprintf(&b, "REPOSITORY GUIDANCE (%s):\n%s\n\n", p, strings.TrimSpace(content))
	}
	return b.String()
}
