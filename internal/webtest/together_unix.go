//go:build unix

package webtest

import (
	"os/exec"
	"syscall"
)

// together has cmd, and every process it starts, end together when its
// context does: killing a browser's first process alone leaves the rest.
func together(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}

// endAll ends what is left of the processes cmd started, once it has
// ended itself.
func endAll(cmd *exec.Cmd) { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
