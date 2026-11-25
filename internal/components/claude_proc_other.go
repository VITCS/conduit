//go:build !unix

package components

import "os/exec"

// ownProcessGroup is a no-op where process groups are unavailable; cancellation
// falls back to killing the direct child.
func ownProcessGroup(cmd *exec.Cmd) {}
