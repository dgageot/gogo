package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/gogo/taskfile"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// newTestApp builds an App wired to byte buffers, with Getwd pinned to dir.
// Pass an empty dir to fall back to os.Getwd (useful for tests that don't need a taskfile).
func newTestApp(t *testing.T, dir string, cliArgs ...string) (*App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	getwd := os.Getwd
	if dir != "" {
		getwd = func() (string, error) { return dir, nil }
	}
	return &App{
		Args:   cliArgs,
		Stdout: &stdout,
		Stderr: &stderr,
		Getwd:  getwd,
	}, &stdout, &stderr
}

func TestIsInternalTask(t *testing.T) {
	assert.False(t, isInternalTask("build"))
	assert.True(t, isInternalTask("_hidden"))
	assert.False(t, isInternalTask("cli:build"))
	assert.True(t, isInternalTask("cli:_hidden"))
	assert.True(t, isInternalTask("cli:utils:_fmt"))
}

func TestVisibleTaskNames(t *testing.T) {
	tf := &taskfile.Taskfile{
		Tasks: map[string]taskfile.Task{
			"build":       {},
			"_internal":   {},
			"cli:test":    {},
			"cli:_helper": {},
		},
	}
	assert.Equal(t, []string{"build", "cli:test"}, visibleTaskNames(tf))
}

func TestAppHelpWritesUsageToStdout(t *testing.T) {
	app, stdout, stderr := newTestApp(t, "", "--help")

	require.NoError(t, app.Run(t.Context()))
	assert.Contains(t, stdout.String(), "gogo - a simple task runner")
	assert.Contains(t, stdout.String(), "--list")
	assert.Empty(t, stderr.String())
}

func TestAppUnknownFlagReturnsError(t *testing.T) {
	app, _, _ := newTestApp(t, "", "--no-such-flag")

	err := app.Run(t.Context())
	require.Error(t, err)
}

func TestAppCompletionBash(t *testing.T) {
	app, stdout, _ := newTestApp(t, "", "--completion", "bash")

	require.NoError(t, app.Run(t.Context()))
	assert.Contains(t, stdout.String(), "_gogo_completions")
	assert.Contains(t, stdout.String(), "complete -F _gogo_completions gogo")
}

func TestAppCompletionZsh(t *testing.T) {
	app, stdout, _ := newTestApp(t, "", "--completion", "zsh")

	require.NoError(t, app.Run(t.Context()))
	assert.Contains(t, stdout.String(), "#compdef gogo")
	assert.Contains(t, stdout.String(), "compdef _gogo gogo")
}

func TestAppCompletionFish(t *testing.T) {
	app, stdout, _ := newTestApp(t, "", "--completion", "fish")

	require.NoError(t, app.Run(t.Context()))
	assert.Contains(t, stdout.String(), "complete -c gogo")
}

func TestAppCompletionUnsupportedShell(t *testing.T) {
	app, _, _ := newTestApp(t, "", "--completion", "pwsh")

	err := app.Run(t.Context())
	assert.EqualError(t, err, "unsupported shell: pwsh (valid: bash, zsh, fish)")
}

func TestAppListShowsDescriptionsAndAliases(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "gogo.yaml"), `version: "1"
tasks:
  # Build the project
  build:
    cmd: go build
  # Run all the tests
  test:
    aliases: [t]
    cmd: go test
  no_desc:
    cmd: echo hi
`)

	app, stdout, _ := newTestApp(t, dir, "--list")

	require.NoError(t, app.Run(t.Context()))
	out := stdout.String()
	assert.Contains(t, out, "build")
	assert.Contains(t, out, "Build the project")
	assert.Contains(t, out, "test")
	assert.Contains(t, out, "Run all the tests")
	assert.Contains(t, out, "(aliases: t)")
	assert.NotContains(t, out, "no_desc", "tasks without a description are omitted")
}

func TestAppListHidesInternalTasks(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "gogo.yaml"), `version: "1"
tasks:
  # Public task
  build:
    cmd: go build
  # Hidden helper
  _internal:
    cmd: echo hidden
`)

	app, stdout, _ := newTestApp(t, dir, "--list")

	require.NoError(t, app.Run(t.Context()))
	out := stdout.String()
	assert.Contains(t, out, "build")
	assert.NotContains(t, out, "_internal")
	assert.NotContains(t, out, "Hidden helper")
}

func TestAppCompleteEmitsVisibleTaskNames(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "gogo.yaml"), `version: "1"
tasks:
  build:
    cmd: go build
  _internal:
    cmd: echo hidden
  test:
    cmd: go test
`)

	app, stdout, _ := newTestApp(t, dir, "--complete")

	require.NoError(t, app.Run(t.Context()))
	assert.Equal(t, "build\ntest\n", stdout.String())
}

func TestAppCompleteSilentOnMissingTaskfile(t *testing.T) {
	app, stdout, stderr := newTestApp(t, t.TempDir(), "--complete")

	require.NoError(t, app.Run(t.Context()))
	assert.Empty(t, stdout.String())
	assert.Empty(t, stderr.String())
}

func TestAppListFailsWhenNoTaskfileFound(t *testing.T) {
	app, _, _ := newTestApp(t, t.TempDir(), "--list")

	err := app.Run(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no gogo.yaml")
}

func TestAppPropagatesGetwdError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	app := &App{
		Args:   []string{"--list"},
		Stdout: &stdout,
		Stderr: &stderr,
		Getwd:  func() (string, error) { return "", io.ErrUnexpectedEOF },
	}

	err := app.Run(t.Context())
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
}
