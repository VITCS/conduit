package seed

import (
	"testing"

	"conduit/internal/validate"
)

// TestSeededWorkflowsValid ensures every seeded workflow is a valid graph
// (no cycles, no dangling edges, known component types).
func TestSeededWorkflowsValid(t *testing.T) {
	for name, errs := range map[string][]string{
		"preview": validate.Workflow(Workflow()),
		"github":  validate.Workflow(GitHubWorkflow()),
		"factory": validate.Workflow(FactoryWorkflow()),
	} {
		if len(errs) != 0 {
			t.Errorf("%s workflow invalid: %v", name, errs)
		}
	}
}

func TestGitHubReviewSeedSetsMaxTokens(t *testing.T) {
	wf := GitHubWorkflow()
	if wf.Rev < 2 {
		t.Fatalf("github workflow rev = %d, want at least 2", wf.Rev)
	}
	for _, n := range wf.Nodes {
		if n.ID != "rev" {
			continue
		}
		if got := n.Field("max_tokens"); got != "4096" {
			t.Fatalf("Agent Review max_tokens = %q, want 4096", got)
		}
		return
	}
	t.Fatal("Agent Review node not found")
}
