package taskfile

import (
	"errors"
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
