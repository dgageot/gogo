package taskfile

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuiltinLookupExposesHome(t *testing.T) {
	r := newTestRunner(t, &Config{}, t.TempDir())

	want, err := os.UserHomeDir()
	require.NoError(t, err)

	got, ok := r.builtinLookup("HOME")
	assert.True(t, ok)
	assert.Equal(t, want, got)
}

func TestRunnerCmdSeesBuiltinHome(t *testing.T) {
	want, err := os.UserHomeDir()
	require.NoError(t, err)

	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"show": {Cmds: []Cmd{{Cmd: "echo {{.HOME}}"}}},
		},
	}
	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("show", ""))

	require.Len(t, *execs, 1)
	assert.Equal(t, "echo "+want, (*execs)[0].Command)
}

func TestRunnerHomeShellSyntaxIsNotExpandedByGogo(t *testing.T) {
	// Shell-style ${HOME} stays untouched: it belongs to the env namespace,
	// the OS already exports HOME, and the downstream shell resolves it.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"show": {Cmds: []Cmd{{Cmd: "echo ${HOME}"}}},
		},
	}
	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("show", ""))

	require.Len(t, *execs, 1)
	assert.Equal(t, "echo ${HOME}", (*execs)[0].Command)
}

func TestRunnerUserVarOverridesBuiltinHome(t *testing.T) {
	dir := t.TempDir()
	tf := &Config{
		Dir:  dir,
		Vars: map[string]Var{"HOME": {Value: "/custom/home"}},
		Tasks: map[string]Task{
			"show": {Cmds: []Cmd{{Cmd: "echo {{.HOME}}"}}},
		},
	}
	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("show", ""))

	require.Len(t, *execs, 1)
	assert.Equal(t, "echo /custom/home", (*execs)[0].Command)
}

func TestRunnerRequiresAcceptsBuiltinHome(t *testing.T) {
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Cmds:     []Cmd{{Cmd: "true"}},
				Requires: Requires{Vars: []string{"HOME"}},
			},
		},
	}
	runner := newTestRunner(t, tf, dir)
	require.NoError(t, runner.Run("build", ""))
}
