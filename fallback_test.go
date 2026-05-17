package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withForeignHooks swaps the package-level lookPath/run hooks for the
// duration of a test. Using package vars (rather than App fields) keeps the
// fallback feature isolated from the rest of the codebase.
func withForeignHooks(t *testing.T, lookPath func(string) (string, error), run func(ctx context.Context, name string, argv []string, dir string, app *App) error) {
	t.Helper()
	prevLook, prevRun := fallbackLookPath, fallbackRun
	fallbackLookPath = lookPath
	fallbackRun = run
	t.Cleanup(func() {
		fallbackLookPath = prevLook
		fallbackRun = prevRun
	})
}

type fakeRun struct {
	called bool
	name   string
	argv   []string
	dir    string
	err    error
}

func (f *fakeRun) run(_ context.Context, name string, argv []string, dir string, _ *App) error {
	f.called = true
	f.name, f.argv, f.dir = name, argv, dir
	return f.err
}

func allFound(string) (string, error)  { return "/usr/bin/stub", nil }
func noneFound(string) (string, error) { return "", exec.ErrNotFound }
func only(name string) func(string) (string, error) {
	return func(n string) (string, error) {
		if n == name {
			return "/usr/bin/" + n, nil
		}
		return "", exec.ErrNotFound
	}
}

func TestFallbackRunsTaskWhenTaskfilePresent(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Taskfile.yml"), []byte("version: '3'\n"), 0o644))

	fr := &fakeRun{}
	withForeignHooks(t, allFound, fr.run)

	app, _, _ := newTestApp(t, dir, "build")
	require.NoError(t, app.Run(t.Context()))
	assert.Equal(t, "task", fr.name)
	assert.Equal(t, []string{"build"}, fr.argv)
	assert.Equal(t, dir, fr.dir)
}

func TestFallbackRunsMise(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mise.toml"), []byte("[tasks.build]\nrun='true'\n"), 0o644))

	fr := &fakeRun{}
	withForeignHooks(t, allFound, fr.run)

	app, _, _ := newTestApp(t, dir, "build")
	require.NoError(t, app.Run(t.Context()))
	assert.Equal(t, "mise", fr.name)
	assert.Equal(t, []string{"run", "build"}, fr.argv)
}

func TestFallbackRunsMake(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Makefile"), []byte("build:\n\t@true\n"), 0o644))

	fr := &fakeRun{}
	withForeignHooks(t, allFound, fr.run)

	app, _, _ := newTestApp(t, dir, "build", "FOO=bar")
	require.NoError(t, app.Run(t.Context()))
	assert.Equal(t, "make", fr.name)
	// `make` consumes trailing args directly — no `--` separator.
	assert.Equal(t, []string{"build", "FOO=bar"}, fr.argv)
}

func TestFallbackDropsDefaultTaskName(t *testing.T) {
	// `gogo` (no positional) must not splice the arg-parser default
	// "default" into the foreign argv — the foreign tool picks its own.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Taskfile.yml"), []byte("version: '3'\n"), 0o644))

	fr := &fakeRun{}
	withForeignHooks(t, allFound, fr.run)

	app, _, _ := newTestApp(t, dir)
	require.NoError(t, app.Run(t.Context()))
	assert.Empty(t, fr.argv)
}

func TestFallbackForwardsCLIArgs(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Taskfile.yml"), []byte("version: '3'\n"), 0o644))

	fr := &fakeRun{}
	withForeignHooks(t, allFound, fr.run)

	app, _, _ := newTestApp(t, dir, "build", "--", "-v", "x")
	require.NoError(t, app.Run(t.Context()))
	assert.Equal(t, []string{"build", "--", "-v", "x"}, fr.argv)
}

func TestFallbackTaskWinsOverMise(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Taskfile.yml"), []byte("version: '3'\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mise.toml"), []byte("[tasks.build]\nrun='true'\n"), 0o644))

	fr := &fakeRun{}
	withForeignHooks(t, allFound, fr.run)

	app, _, _ := newTestApp(t, dir, "build")
	require.NoError(t, app.Run(t.Context()))
	assert.Equal(t, "task", fr.name)
}

func TestFallbackSkipsRunnerNotOnPath(t *testing.T) {
	// Both files exist but only `mise` is installed. We must skip the
	// uninstalled `task` runner instead of stopping at the first match.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Taskfile.yml"), []byte("version: '3'\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mise.toml"), []byte("[tasks.build]\nrun='true'\n"), 0o644))

	fr := &fakeRun{}
	withForeignHooks(t, only("mise"), fr.run)

	app, _, _ := newTestApp(t, dir, "build")
	require.NoError(t, app.Run(t.Context()))
	assert.Equal(t, "mise", fr.name)
}

func TestFallbackWalksUp(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "Taskfile.yml"), []byte("version: '3'\n"), 0o644))
	sub := filepath.Join(root, "a", "b")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	fr := &fakeRun{}
	withForeignHooks(t, allFound, fr.run)

	app, _, _ := newTestApp(t, sub, "build")
	require.NoError(t, app.Run(t.Context()))
	assert.Equal(t, root, fr.dir)
}

func TestFallbackBinaryMissingFallsThrough(t *testing.T) {
	// Taskfile present, `task` not installed, no other runner: the
	// regular "no gogo.yaml" error must still surface.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Taskfile.yml"), []byte("version: '3'\n"), 0o644))

	fr := &fakeRun{}
	withForeignHooks(t, noneFound, fr.run)

	app, _, _ := newTestApp(t, dir, "build")
	err := app.Run(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no gogo.yaml")
	assert.False(t, fr.called)
}

func TestFallbackGogoYamlWins(t *testing.T) {
	// A real gogo.yaml short-circuits the fallback path entirely.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gogo.yaml"), []byte(`version: "1"
tasks:
  build:
    cmd: echo built
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Taskfile.yml"), []byte("version: '3'\n"), 0o644))

	fr := &fakeRun{}
	withForeignHooks(t, allFound, fr.run)

	app, _, _ := newTestApp(t, dir, "--dry", "build")
	require.NoError(t, app.Run(t.Context()))
	assert.False(t, fr.called)
}

func TestFallbackPropagatesError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Taskfile.yml"), []byte("version: '3'\n"), 0o644))

	want := errors.New("boom")
	fr := &fakeRun{err: want}
	withForeignHooks(t, allFound, fr.run)

	app, _, _ := newTestApp(t, dir, "build")
	require.ErrorIs(t, app.Run(t.Context()), want)
}
