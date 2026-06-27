package cops

import (
	"testing"

	"github.com/dgageot/rubocop-go/coptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShellViaRunnerFlagsExecCommand(t *testing.T) {
	src := `package taskfile
import "os/exec"
func run() { _ = exec.Command("ls") }
`
	offenses := coptest.RunNamed(t, NewShellViaRunner(), "taskfile/runner.go", src)
	require.Len(t, offenses, 1)
	assert.Equal(t, "Gogo/ShellViaRunner", offenses[0].CopName)
}

func TestShellViaRunnerAllowsShellLayer(t *testing.T) {
	src := `package taskfile
import "os/exec"
func run() { _ = exec.Command("ls") }
`
	// shell.go and fallback.go are the sanctioned callers.
	assert.Empty(t, coptest.RunNamed(t, NewShellViaRunner(), "taskfile/shell.go", src))
	assert.Empty(t, coptest.RunNamed(t, NewShellViaRunner(), "fallback.go", src))
}

func TestShellViaRunnerSkipsTests(t *testing.T) {
	src := `package taskfile
import "os/exec"
func run() { _ = exec.Command("ls") }
`
	assert.Empty(t, coptest.RunNamed(t, NewShellViaRunner(), "taskfile/runner_test.go", src))
}
