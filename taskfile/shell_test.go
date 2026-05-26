package taskfile

import (
	"bytes"
	"errors"
	"os"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultShellRunnerOutputSurfacesStderr(t *testing.T) {
	// A failing `sh:` lookup must put the underlying tool's stderr into
	// the error message — otherwise users only see "exit status 1" and
	// have no idea what went wrong.
	_, err := newDefaultShellRunner().Output(ShellCommand{
		Kind:    ShellCommandVar,
		Command: `echo "boom: missing config" >&2; exit 1`,
		Dir:     t.TempDir(),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom: missing config")
}

func TestDefaultShellRunnerOutputReturnsStdoutOnSuccess(t *testing.T) {
	out, err := newDefaultShellRunner().Output(ShellCommand{
		Kind:    ShellCommandVar,
		Command: `printf hello`,
		Dir:     t.TempDir(),
	})
	require.NoError(t, err)
	assert.Equal(t, "hello", string(out))
}

func TestDefaultShellRunnerCachesOpLookup(t *testing.T) {
	// op:// secrets trigger an exec.LookPath("op") on every cmd. The lookup
	// is cached on the Runner-bound shell runner so a 50-cmd task with
	// secrets doesn't re-walk PATH 50 times.
	calls := 0
	s := &defaultShellRunner{
		opPath: sync.OnceValues(func() (string, error) {
			calls++
			return "", errors.New("op not installed")
		}),
	}

	for range 5 {
		_, err := s.shellExecCommand(ShellCommand{UseOpRun: true, Command: "true"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "1Password CLI")
	}
	assert.Equal(t, 1, calls, "op lookup must only happen once per runner")
}

func TestOpRunUsesNoMaskingWhenStdioIsATerminal(t *testing.T) {
	// `op run` masks secrets in stdout/stderr by piping them through itself,
	// which strips the TTY and breaks any interactive TUI the command may
	// launch (docker agent eval, fzf, less, vim, …). When both streams are
	// terminals we pass --no-masking so op leaves them untouched.
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no /dev/tty available: %v", err)
	}
	t.Cleanup(func() { _ = tty.Close() })

	args := opRunArgs(ShellCommand{
		Command: "echo hi",
		Stdout:  tty,
		Stderr:  tty,
	})
	assert.Contains(t, args, "--no-masking")
	assert.Equal(t, []string{"--", "/bin/sh", "-c", "echo hi"}, args[len(args)-4:])
}

func TestOpRunKeepsMaskingWhenStdioIsNotATerminal(t *testing.T) {
	// In CI / scripts (output redirected to a buffer or a file) we keep the
	// default masking so accidental secret leaks into logs are still
	// concealed.
	args := opRunArgs(ShellCommand{
		Command: "echo hi",
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
	})
	assert.False(t, slices.Contains(args, "--no-masking"))
	assert.Equal(t, []string{"run", "--", "/bin/sh", "-c", "echo hi"}, args)
}

func TestOpRunKeepsMaskingWhenOnlyStderrIsATerminal(t *testing.T) {
	// Half-redirected: piping just stdout (e.g. `gogo evals | tee log`) is a
	// non-interactive use, so masking stays on.
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no /dev/tty available: %v", err)
	}
	t.Cleanup(func() { _ = tty.Close() })

	args := opRunArgs(ShellCommand{
		Command: "echo hi",
		Stdout:  &bytes.Buffer{},
		Stderr:  tty,
	})
	assert.False(t, slices.Contains(args, "--no-masking"))
}
