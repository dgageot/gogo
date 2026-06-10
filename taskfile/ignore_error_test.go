package taskfile

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIgnoreErrorContinuesExecution(t *testing.T) {
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Cmds: []Cmd{
					{Cmd: "echo step1"},
					{Cmd: "echo failing", IgnoreError: true},
					{Cmd: "echo step3"},
				},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	var execs []string
	runner.ShellRunner = &fakeShellRunner{
		runFunc: func(req ShellCommand) error {
			execs = append(execs, req.Command)
			if req.Command == "echo failing" {
				return errors.New("boom")
			}
			return nil
		},
	}

	err := runner.Run("build", "")
	require.NoError(t, err)
	assert.Equal(t, []string{"echo step1", "echo failing", "echo step3"}, execs)
}

func TestIgnoreErrorIsPerCmd(t *testing.T) {
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Cmds: []Cmd{
					{Cmd: "echo failing", IgnoreError: true},
					{Cmd: "echo also-failing"},
					{Cmd: "echo never-runs"},
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
			return boom
		},
	}

	err := runner.Run("build", "")
	require.ErrorIs(t, err, boom)
	assert.Equal(t, []string{"echo failing", "echo also-failing"}, execs)
}
