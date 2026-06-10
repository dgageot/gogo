package taskfile

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// promptConfig returns a config with a guarded "deploy" task and its "build" dep.
func promptConfig(dir string) *Config {
	return &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"deploy": {
				Prompt: "Deploy to production?",
				Deps:   []Dep{{Task: "build"}},
				Cmds:   []Cmd{{Cmd: "echo deploying"}},
			},
			"build": {
				Cmds: []Cmd{{Cmd: "echo building"}},
			},
		},
		DotenvVars: make(map[string]string),
	}
}

func TestPromptYesRunsTask(t *testing.T) {
	dir := t.TempDir()
	runner := newTestRunner(t, promptConfig(dir), dir)
	runner.IO.Stdin = strings.NewReader("y\n")
	var stderr strings.Builder
	runner.IO.Stderr = &stderr
	execs := captureExecs(runner)

	err := runner.Run("deploy", "")
	require.NoError(t, err)

	assert.Contains(t, stderr.String(), "Deploy to production? [y/N]:")
	require.Len(t, *execs, 2)
}

func TestPromptDoesNotConsumeTaskStdin(t *testing.T) {
	dir := t.TempDir()
	runner := newTestRunner(t, promptConfig(dir), dir)
	stdin := strings.NewReader("y\npiped input for the task")
	runner.IO.Stdin = stdin
	runner.IO.Stderr = &strings.Builder{}

	err := runner.Run("deploy", "")
	require.NoError(t, err)

	rest, err := io.ReadAll(stdin)
	require.NoError(t, err)
	assert.Equal(t, "piped input for the task", string(rest),
		"the prompt must read only up to the newline; the rest belongs to the task")
}

func TestPromptDeclineAbortsBeforeDeps(t *testing.T) {
	dir := t.TempDir()
	runner := newTestRunner(t, promptConfig(dir), dir)
	runner.IO.Stdin = strings.NewReader("n\n")
	runner.IO.Stderr = &strings.Builder{}
	execs := captureExecs(runner)

	err := runner.Run("deploy", "")
	require.ErrorContains(t, err, `task "deploy": prompt declined`)
	assert.Empty(t, *execs, "declining must leave deps and cmds unexecuted")
}

func TestPromptEOFDeclines(t *testing.T) {
	dir := t.TempDir()
	runner := newTestRunner(t, promptConfig(dir), dir)
	runner.IO.Stdin = strings.NewReader("")
	runner.IO.Stderr = &strings.Builder{}

	err := runner.Run("deploy", "")
	require.ErrorContains(t, err, "prompt declined")
}

func TestPromptNilStdinDeclines(t *testing.T) {
	dir := t.TempDir()
	runner := newTestRunner(t, promptConfig(dir), dir)
	runner.IO.Stdin = nil
	runner.IO.Stderr = &strings.Builder{}

	err := runner.Run("deploy", "")
	require.ErrorContains(t, err, "prompt declined")
}

func TestPromptAssumeYesSkipsQuestion(t *testing.T) {
	dir := t.TempDir()
	runner := newTestRunner(t, promptConfig(dir), dir)
	runner.AssumeYes = true
	runner.IO.Stdin = nil // must not be read
	var stderr strings.Builder
	runner.IO.Stderr = &stderr
	execs := captureExecs(runner)

	err := runner.Run("deploy", "")
	require.NoError(t, err)

	assert.NotContains(t, stderr.String(), "[y/N]")
	require.Len(t, *execs, 2)
}

func TestPromptDryRunSkipsQuestion(t *testing.T) {
	dir := t.TempDir()
	runner := newTestRunner(t, promptConfig(dir), dir)
	runner.DryRun = true
	runner.IO.Stdin = nil // must not be read
	var stderr strings.Builder
	runner.IO.Stderr = &stderr
	execs := captureExecs(runner)

	err := runner.Run("deploy", "")
	require.NoError(t, err)

	assert.NotContains(t, stderr.String(), "[y/N]")
	assert.Empty(t, *execs)
}
