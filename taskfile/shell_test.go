package taskfile

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultShellRunnerOutputSurfacesStderr(t *testing.T) {
	// A failing `sh:` lookup must put the underlying tool's stderr into
	// the error message — otherwise users only see "exit status 1" and
	// have no idea what went wrong.
	_, err := defaultShellRunner{}.Output(ShellCommand{
		Kind:    ShellCommandVar,
		Command: `echo "boom: missing config" >&2; exit 1`,
		Dir:     t.TempDir(),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom: missing config")
}

func TestDefaultShellRunnerOutputReturnsStdoutOnSuccess(t *testing.T) {
	out, err := defaultShellRunner{}.Output(ShellCommand{
		Kind:    ShellCommandVar,
		Command: `printf hello`,
		Dir:     t.TempDir(),
	})
	require.NoError(t, err)
	assert.Equal(t, "hello", string(out))
}
