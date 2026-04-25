package taskfile

import (
	"fmt"
	"io"
	"os/exec"
)

// ShellCommandKind identifies why a shell command is being run.
type ShellCommandKind string

const (
	ShellCommandTask         ShellCommandKind = "task"
	ShellCommandPrecondition ShellCommandKind = "precondition"
	ShellCommandVar          ShellCommandKind = "var"
)

// ShellCommand describes one shell invocation.
type ShellCommand struct {
	Kind     ShellCommandKind
	TaskName string
	Command  string
	Dir      string
	Env      []string
	UseOpRun bool
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
}

// ShellRunner runs shell commands for task commands, preconditions, and shell variables.
type ShellRunner interface {
	Run(req ShellCommand) error
	Output(req ShellCommand) ([]byte, error)
}

type defaultShellRunner struct{}

func (defaultShellRunner) Run(req ShellCommand) error {
	cmd, err := shellExecCommand(req)
	if err != nil {
		return err
	}
	cmd.Stdin = req.Stdin
	cmd.Stdout = req.Stdout
	cmd.Stderr = req.Stderr
	return cmd.Run()
}

func (defaultShellRunner) Output(req ShellCommand) ([]byte, error) {
	cmd, err := shellExecCommand(req)
	if err != nil {
		return nil, err
	}
	return cmd.Output()
}

func shellExecCommand(req ShellCommand) (*exec.Cmd, error) {
	if req.UseOpRun {
		if _, err := exec.LookPath("op"); err != nil {
			return nil, fmt.Errorf("uses op:// secrets but the 1Password CLI (op) is not installed: %w\n\nInstall it from https://developer.1password.com/docs/cli/get-started/", err)
		}
		return configuredShellCommand(exec.Command("op", "run", "--", "/bin/sh", "-c", req.Command), req), nil
	}
	return configuredShellCommand(exec.Command("/bin/sh", "-c", req.Command), req), nil
}

func configuredShellCommand(cmd *exec.Cmd, req ShellCommand) *exec.Cmd {
	cmd.Dir = req.Dir
	cmd.Env = req.Env
	return cmd
}
