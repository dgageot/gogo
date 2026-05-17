package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeExec captures one shell-out call so tests can assert which fallback
// runner was selected without spawning real processes.
type fakeExec struct {
	called bool
	name   string
	args   []string
	dir    string
	err    error
}

func (f *fakeExec) run(_ context.Context, name string, args []string, dir string, _, _ io.Writer) error {
	f.called = true
	f.name = name
	f.args = args
	f.dir = dir
	return f.err
}

// alwaysFound is a LookPath stub that pretends every binary is on PATH.
func alwaysFound(string) (string, error) { return "/usr/bin/stub", nil }

// onlyFound returns a LookPath stub that resolves only the named binary;
// every other lookup returns exec.ErrNotFound.
func onlyFound(allowed string) func(string) (string, error) {
	return func(name string) (string, error) {
		if name == allowed {
			return "/usr/bin/" + name, nil
		}
		return "", exec.ErrNotFound
	}
}

// neverFound is a LookPath stub that simulates an empty PATH.
func neverFound(string) (string, error) { return "", exec.ErrNotFound }

func newFallbackApp(t *testing.T, dir string, fx *fakeExec, lookPath func(string) (string, error), cliArgs ...string) (*App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	app, stdout, stderr := newTestApp(t, dir, cliArgs...)
	app.LookPath = lookPath
	app.RunCommand = fx.run
	return app, stdout, stderr
}

func TestFallbackRunsTaskWhenTaskfilePresent(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Taskfile.yml"), "version: '3'\n")

	fx := &fakeExec{}
	app, _, _ := newFallbackApp(t, dir, fx, alwaysFound, "build")

	require.NoError(t, app.Run(t.Context()))
	assert.True(t, fx.called, "expected the fallback runner to be invoked")
	assert.Equal(t, "task", fx.name)
	assert.Equal(t, []string{"build"}, fx.args)
	assert.Equal(t, dir, fx.dir)
}

func TestFallbackTaskDropsDefaultPositional(t *testing.T) {
	// `gogo` (no positional) must not splice the arg-parser default
	// "default" into the task argv — we want `task` to pick its own
	// default, which is what running it with no args does.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Taskfile.yml"), "version: '3'\n")

	fx := &fakeExec{}
	app, _, _ := newFallbackApp(t, dir, fx, alwaysFound)

	require.NoError(t, app.Run(t.Context()))
	assert.Equal(t, "task", fx.name)
	assert.Empty(t, fx.args, "default task name must not be forwarded")
}

func TestFallbackTaskForwardsCLIArgsAfterDoubleDash(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Taskfile.yml"), "version: '3'\n")

	fx := &fakeExec{}
	app, _, _ := newFallbackApp(t, dir, fx, alwaysFound, "build", "--", "-v", "x")

	require.NoError(t, app.Run(t.Context()))
	assert.Equal(t, []string{"build", "--", "-v", "x"}, fx.args)
}

func TestFallbackRunsMiseWhenMiseTomlPresent(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mise.toml"), "[tasks.build]\nrun = 'true'\n")

	fx := &fakeExec{}
	app, _, _ := newFallbackApp(t, dir, fx, alwaysFound, "build")

	require.NoError(t, app.Run(t.Context()))
	assert.Equal(t, "mise", fx.name)
	assert.Equal(t, []string{"run", "build"}, fx.args)
	assert.Equal(t, dir, fx.dir)
}

func TestFallbackMiseDropsDefaultPositional(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mise.toml"), "[tasks.build]\nrun = 'true'\n")

	fx := &fakeExec{}
	app, _, _ := newFallbackApp(t, dir, fx, alwaysFound)

	require.NoError(t, app.Run(t.Context()))
	assert.Equal(t, []string{"run"}, fx.args)
}

func TestFallbackTaskWinsOverMiseInSameDir(t *testing.T) {
	// Both files exist; the order in fallbackRunners makes `task` win.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Taskfile.yml"), "version: '3'\n")
	writeFile(t, filepath.Join(dir, "mise.toml"), "[tasks.build]\nrun = 'true'\n")

	fx := &fakeExec{}
	app, _, _ := newFallbackApp(t, dir, fx, alwaysFound, "build")

	require.NoError(t, app.Run(t.Context()))
	assert.Equal(t, "task", fx.name)
}

func TestFallbackWalksUpFromSubdirectory(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "Taskfile.yml"), "version: '3'\n")
	sub := filepath.Join(root, "sub", "deeper")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	fx := &fakeExec{}
	app, _, _ := newFallbackApp(t, sub, fx, alwaysFound, "build")

	require.NoError(t, app.Run(t.Context()))
	assert.Equal(t, "task", fx.name)
	assert.Equal(t, root, fx.dir, "fallback should run in the directory holding the task file")
}

func TestFallbackSilentlyIgnoresWhenBinaryMissing(t *testing.T) {
	// A Taskfile is present but `task` is not on PATH: gogo doesn't try to
	// run it (no "task: command not found" noise) and falls through to the
	// regular "no gogo.yaml" error, which tells the user what to install or
	// create.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Taskfile.yml"), "version: '3'\n")

	fx := &fakeExec{}
	app, _, _ := newFallbackApp(t, dir, fx, neverFound, "build")

	err := app.Run(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no gogo.yaml")
	assert.False(t, fx.called)
}

func TestFallbackUsesMiseWhenOnlyMiseInstalled(t *testing.T) {
	// Both files are present but only `mise` is installed. Gogo skips the
	// uninstalled `task` runner instead of stopping at the first match,
	// so the user still gets useful behavior.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Taskfile.yml"), "version: '3'\n")
	writeFile(t, filepath.Join(dir, "mise.toml"), "[tasks.build]\nrun = 'true'\n")

	fx := &fakeExec{}
	app, _, _ := newFallbackApp(t, dir, fx, onlyFound("mise"), "build")

	require.NoError(t, app.Run(t.Context()))
	assert.Equal(t, "mise", fx.name)
	assert.Equal(t, []string{"run", "build"}, fx.args)
}

func TestFallbackNotTriggeredWhenGogoYamlExists(t *testing.T) {
	// A real gogo.yaml short-circuits the fallback path entirely; the
	// stray Taskfile in the same dir is ignored.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "gogo.yaml"), `version: "1"
tasks:
  build:
    cmd: echo built
`)
	writeFile(t, filepath.Join(dir, "Taskfile.yml"), "version: '3'\n")

	fx := &fakeExec{}
	app, _, _ := newFallbackApp(t, dir, fx, alwaysFound, "--dry", "build")

	require.NoError(t, app.Run(t.Context()))
	assert.False(t, fx.called, "gogo.yaml must take precedence over fallback runners")
}

func TestFallbackPropagatesExecError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Taskfile.yml"), "version: '3'\n")

	wantErr := errors.New("boom")
	fx := &fakeExec{err: wantErr}
	app, _, _ := newFallbackApp(t, dir, fx, alwaysFound, "build")

	err := app.Run(t.Context())
	require.ErrorIs(t, err, wantErr)
}

func TestFallbackNoFileFoundReturnsOriginalError(t *testing.T) {
	// Neither a gogo.yaml nor any fallback file exists: gogo must surface
	// the regular "no gogo.yaml" error so the user knows what to do.
	dir := t.TempDir()

	fx := &fakeExec{}
	app, _, _ := newFallbackApp(t, dir, fx, alwaysFound, "build")

	err := app.Run(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no gogo.yaml")
	assert.False(t, fx.called)
}
