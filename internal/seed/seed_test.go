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
