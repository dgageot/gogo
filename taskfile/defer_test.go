package taskfile

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeferRunsAfterCmdsInReverseOrder(t *testing.T) {
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Cmds: []Cmd{
					{Defer: "echo cleanup1"},
					{Cmd: "echo step1"},
					{Defer: "echo cleanup2"},
					{Cmd: "echo step2"},
				},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("build", "")
	require.NoError(t, err)

	var cmds []string
	for _, e := range *execs {
		cmds = append(cmds, e.Command)
	}
	assert.Equal(t, []string{"echo step1", "echo step2", "echo cleanup2", "echo cleanup1"}, cmds)
}

func TestDeferRunsWhenCmdFails(t *testing.T) {
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Cmds: []Cmd{
					{Defer: "echo cleanup"},
					{Cmd: "echo failing"},
					{Defer: "echo never-registered"},
				},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	boom := errors.New("boom")
	var execs []string
	runner.ShellRunner = &fakeShellRunner{
		runFunc: func(req ShellCommand) error {
			execs = append(execs, req.Command)
			if req.Command == "echo failing" {
				return boom
			}
			return nil
		},
	}

	err := runner.Run("build", "")
	require.ErrorIs(t, err, boom)
	// The defer registered before the failure runs; the one after doesn't.
	assert.Equal(t, []string{"echo failing", "echo cleanup"}, execs)
}

func TestDeferFailureDoesNotFailTask(t *testing.T) {
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Cmds: []Cmd{
					{Defer: "exit 1"},
					{Cmd: "echo step"},
				},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	runner.ShellRunner = &fakeShellRunner{
		runFunc: func(req ShellCommand) error {
			if req.Command == "exit 1" {
				return errors.New("cleanup failed")
			}
			return nil
		},
	}

	err := runner.Run("build", "")
	assert.NoError(t, err, "a failed deferred command must not fail the task")
}

func TestDeferExpandsVars(t *testing.T) {
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Vars: map[string]Var{"TMP": {Value: "scratch"}},
				Cmds: []Cmd{
					{Defer: "rm -rf {{.TMP}}"},
					{Cmd: "echo step"},
				},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("build", "")
	require.NoError(t, err)

	require.Len(t, *execs, 2)
	assert.Equal(t, "rm -rf scratch", (*execs)[1].Command)
}

func TestDeferDryRunDoesNotExecute(t *testing.T) {
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Cmds: []Cmd{
					{Defer: "echo cleanup"},
					{Cmd: "echo step"},
				},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	runner.DryRun = true
	execs := captureExecs(runner)

	err := runner.Run("build", "")
	require.NoError(t, err)
	assert.Empty(t, *execs)
}
