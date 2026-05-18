package taskfile

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
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
	out, err := cmd.Output()
	if err != nil {
		return out, withStderr(err)
	}
	return out, nil
}

// withStderr enriches a command failure with the captured stderr. cmd.Output
// populates *exec.ExitError.Stderr automatically when cmd.Stderr is nil, but
// fmt.Errorf("%w", err) only renders ExitError.Error() ("exit status 1")
// which strips that context. Re-wrapping puts stderr back into the message
// so users debugging a broken `sh:` lookup or a missing git repo see what
// the underlying tool actually said.
func withStderr(err error) error {
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return err
	}
	stderr := strings.TrimSpace(string(ee.Stderr))
	if stderr == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, stderr)
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
