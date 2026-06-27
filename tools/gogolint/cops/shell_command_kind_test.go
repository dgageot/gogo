package cops

import (
	"testing"

	"github.com/dgageot/rubocop-go/coptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShellCommandKindRequiredFlagsMissingKind(t *testing.T) {
	src := `package taskfile
type ShellCommand struct { Kind string; Command string }
func f() ShellCommand { return ShellCommand{Command: "ls"} }
`
	offenses := coptest.RunNamed(t, NewShellCommandKindRequired(), "taskfile/runner.go", src)
	require.Len(t, offenses, 1)
	assert.Equal(t, "Gogo/ShellCommandKindRequired", offenses[0].CopName)
}

func TestShellCommandKindRequiredAllowsKind(t *testing.T) {
	src := `package taskfile
type ShellCommand struct { Kind string; Command string }
func f() ShellCommand { return ShellCommand{Kind: "task", Command: "ls"} }
`
	assert.Empty(t, coptest.RunNamed(t, NewShellCommandKindRequired(), "taskfile/runner.go", src))
}

func TestShellCommandKindRequiredSkipsTests(t *testing.T) {
	src := `package taskfile
type ShellCommand struct { Kind string; Command string }
func f() ShellCommand { return ShellCommand{Command: "ls"} }
`
	assert.Empty(t, coptest.RunNamed(t, NewShellCommandKindRequired(), "taskfile/runner_test.go", src))
}
