package taskfile

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// statusRuns returns the status-kind shell commands recorded by the fake runner.
func statusRuns(t *testing.T, r *Runner) []ShellCommand {
	t.Helper()
	shell, ok := r.ShellRunner.(*fakeShellRunner)
	require.True(t, ok)

	var status []ShellCommand
	for _, req := range shell.runsSnapshot() {
		if req.Kind == ShellCommandStatus {
			status = append(status, req)
		}
	}
	return status
}

func TestStatusPassingSkipsTask(t *testing.T) {
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"create-bucket": {
				Status: StringList{"true"},
				Cmds:   []Cmd{{Cmd: "echo creating"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("create-bucket", "")
	require.NoError(t, err)
	assert.Empty(t, *execs, "all status commands passed, the task is up to date")
}

func TestStatusFailingRunsTask(t *testing.T) {
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"create-bucket": {
				Status: StringList{"false"},
				Cmds:   []Cmd{{Cmd: "echo creating"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("create-bucket", "")
	require.NoError(t, err)
	require.Len(t, *execs, 1)
	assert.Equal(t, "echo creating", (*execs)[0].Command)
}

func TestStatusStopsAtFirstFailure(t *testing.T) {
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"install": {
				Status: StringList{"false", "true"},
				Cmds:   []Cmd{{Cmd: "echo installing"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("install", "")
	require.NoError(t, err)
	assert.Len(t, *execs, 1, "a failing status command means the task runs")

	status := statusRuns(t, runner)
	require.Len(t, status, 1, "the first failing status command short-circuits")
	assert.Equal(t, "false", status[0].Command)
}

func TestStatusForceBypassesCheck(t *testing.T) {
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"create-bucket": {
				Status: StringList{"true"},
				Cmds:   []Cmd{{Cmd: "echo creating"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	runner.Force = true
	execs := captureExecs(runner)

	err := runner.Run("create-bucket", "")
	require.NoError(t, err)
	assert.Len(t, *execs, 1, "--force runs the task regardless of status")
	assert.Empty(t, statusRuns(t, runner), "--force doesn't even probe")
}

func TestStatusAndSourcesBothMustAgree(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"main.go": "package main"})

	statusCmd := "true"
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Sources: StringList{"*.go"},
				Status:  StringList{"check-state"},
				Cmds:    []Cmd{{Cmd: "echo building"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	shell, ok := runner.ShellRunner.(*fakeShellRunner)
	require.True(t, ok)
	shell.runFunc = func(req ShellCommand) error {
		if req.Command == "check-state" && statusCmd == "false" {
			return assert.AnError
		}
		return nil
	}
	execs := captureExecs(runner)

	// First run: no stored checksum, sources are stale — runs without probing status.
	require.NoError(t, runner.Run("build", ""))
	require.Len(t, *execs, 1)
	assert.Empty(t, statusRuns(t, runner), "stale sources already force a run")

	// Second run: sources up to date and status passes — skipped.
	runner.ResetRan()
	require.NoError(t, runner.Run("build", ""))
	assert.Len(t, *execs, 1)

	// Third run: sources still up to date but status fails — runs.
	statusCmd = "false"
	runner.ResetRan()
	require.NoError(t, runner.Run("build", ""))
	assert.Len(t, *execs, 2, "a failing status overrides fresh sources")
}

func TestStatusUsesOpRunForSecrets(t *testing.T) {
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"deploy": {
				Env:    map[string]string{"TOKEN": "op://vault/item/field"},
				Status: StringList{"true"},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)

	err := runner.Run("deploy", "")
	require.NoError(t, err)

	status := statusRuns(t, runner)
	require.Len(t, status, 1)
	assert.True(t, status[0].UseOpRun, "status probes must see resolved op:// secrets")
}

func TestStatusRunsDuringDryRun(t *testing.T) {
	// Like preconditions, status probes are read-only checks: they still run
	// under --dry so the printed plan reflects what a real run would skip.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"create-bucket": {
				Status: StringList{"true"},
				Cmds:   []Cmd{{Cmd: "echo creating"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	runner.DryRun = true
	execs := captureExecs(runner)

	err := runner.Run("create-bucket", "")
	require.NoError(t, err)
	assert.Len(t, statusRuns(t, runner), 1)
	assert.Empty(t, *execs)
}

func TestStatusReceivesTaskEnv(t *testing.T) {
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"deploy": {
				Env:    map[string]string{"BUCKET": "artifacts"},
				Status: StringList{"true"},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)

	err := runner.Run("deploy", "")
	require.NoError(t, err)

	status := statusRuns(t, runner)
	require.Len(t, status, 1)
	assert.Equal(t, "artifacts", envValue(status[0].Env, "BUCKET"))
}
