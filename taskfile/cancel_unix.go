//go:build darwin || linux

package taskfile

import (
	"errors"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

func configureCancellation(cmd *exec.Cmd, req ShellCommand) {
	// Auxiliary commands may open /dev/tty even when their stdio is nil.
	// Preserve an existing foreground group; stream inspection alone cannot
	// tell whether a command can interact with the controlling terminal.
	if hasForegroundTerminal() {
		return
	}
	// Explicit terminal streams also retain descriptor inheritance.
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

func hasForegroundTerminal() bool {
	tty, err := os.Open("/dev/tty")
	if err != nil {
		return false
	}
	defer tty.Close()
	foreground, err := unix.IoctlGetInt(int(tty.Fd()), unix.TIOCGPGRP)
	return err == nil && foreground == unix.Getpgrp()
}
