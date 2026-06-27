package cops

import (
	"testing"

	"github.com/dgageot/rubocop-go/coptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCloneStashedEnvFlagsAliasedSlice(t *testing.T) {
	src := `package taskfile
import "slices"
var _ = slices.Clone[[]string]
type ShellCommand struct { Env []string }
func f(req ShellCommand, env []string) ShellCommand {
	req.Env = env
	return req
}
`
	offenses := coptest.RunTyped(t, NewCloneStashedEnv(), src)
	require.Len(t, offenses, 1)
	assert.Equal(t, "Gogo/CloneStashedEnv", offenses[0].CopName)
}

func TestCloneStashedEnvAllowsClone(t *testing.T) {
	src := `package taskfile
import "slices"
type ShellCommand struct { Env []string }
func f(req ShellCommand) ShellCommand {
	req.Env = slices.Clone(req.Env)
	return req
}
`
	assert.Empty(t, coptest.RunTyped(t, NewCloneStashedEnv(), src))
}

func TestCloneStashedEnvIgnoresOtherTypes(t *testing.T) {
	// Assigning to a non-ShellCommand .Env field (e.g. exec.Cmd) is unrelated.
	src := `package taskfile
type Cmd struct { Env []string }
func f(c *Cmd, env []string) { c.Env = env }
`
	assert.Empty(t, coptest.RunTyped(t, NewCloneStashedEnv(), src))
}
