//go:build darwin || linux

package taskfile

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func configureCancellation(cmd *exec.Cmd, req ShellCommand) {
	// A new process group must not steal interactive tasks from the terminal's
	// foreground group. Non-interactive commands can safely own a group.
	if f, ok := req.Stdin.(*os.File); ok && isTerminal(f) {
		return
	}
	if isTerminal(req.Stdout) || isTerminal(req.Stderr) {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
