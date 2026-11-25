//go:build unix

package components

import (
	"os/exec"
	"syscall"
)

// ownProcessGroup starts cmd in its own process group and makes cancellation kill
// the whole group, so tools a Claude Code session started (test runners, builds)
// stop with it instead of lingering.
func ownProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
