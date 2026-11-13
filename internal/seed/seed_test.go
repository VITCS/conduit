package seed

import (
	"strings"
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

func TestGitHubReviewSeedCarriesReviewContext(t *testing.T) {
	wf := GitHubWorkflow()
	if wf.Rev < 3 {
		t.Fatalf("github workflow rev = %d, want at least 3", wf.Rev)
	}
	seen := map[string]bool{}
	for _, n := range wf.Nodes {
		switch n.ID {
		case "rev":
			seen["rev"] = true
			if got := n.Field("max_tokens"); got != "4096" {
				t.Fatalf("Agent Review max_tokens = %q, want 4096", got)
			}
		case "scan":
			seen["scan"] = true
			if got := n.Field("repo_url"); got != "https://github.com/${memory.pr_repo}" {
				t.Fatalf("Security Scan repo_url = %q, want memory pr_repo", got)
			}
			if got := n.Field("ref"); got != "${memory.pr_head}" {
				t.Fatalf("Security Scan ref = %q, want memory pr_head", got)
			}
		case "appr":
			seen["appr"] = true
			sum := n.Field("summary")
			for _, want := range []string{"${memory.pr_repo}", "${memory.pr_number}", "${memory.review}", "${memory.diff_preview}", "${memory.security_summary}"} {
				if !strings.Contains(sum, want) {
					t.Fatalf("Human Approval summary missing %s: %q", want, sum)
				}
			}
		case "cmt", "merge":
			seen[n.ID] = true
			if got := n.Field("repo"); got != "${memory.pr_repo}" {
				t.Fatalf("%s repo = %q, want memory pr_repo", n.ID, got)
			}
			if got := n.Field("number"); got != "${memory.pr_number}" {
				t.Fatalf("%s number = %q, want memory pr_number", n.ID, got)
			}
		}
	}
	for _, id := range []string{"rev", "scan", "appr", "cmt", "merge"} {
		if !seen[id] {
			t.Fatalf("%s node not found", id)
		}
	}
}
