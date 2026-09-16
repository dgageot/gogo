//go:build !darwin && !linux

package taskfile

import "os/exec"

func configureCancellation(_ *exec.Cmd, _ ShellCommand) {
	// CommandContext kills the child; WaitDelay bounds inherited-pipe waits.
}
