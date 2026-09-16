package taskfile

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectSourcesPreservesPerTaskDir(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "sub")
	writeFiles(t, dir, map[string]string{
		"main.go":    "package main",
		"sub/lib.go": "package lib",
	})

	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Sources: StringList{"*.go"},
				Deps:    []Dep{{Task: "lib"}},
				Cmds:    []Cmd{{Cmd: "go build"}},
			},
			"lib": {
				Dir:     "sub",
				Sources: StringList{"*.go"},
				Cmds:    []Cmd{{Cmd: "go build ./lib"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	sources := runner.collectSources("build", make(map[string]struct{}))

	require.Len(t, sources, 2)
	assert.Equal(t, dir, sources[0].Dir)
	assert.Equal(t, []string{"*.go"}, sources[0].Patterns)
	assert.Equal(t, subDir, sources[1].Dir)
	assert.Equal(t, []string{"*.go"}, sources[1].Patterns)
}

func TestCollectSourcesNoDeps(t *testing.T) {
	dir := t.TempDir()

	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Sources: StringList{"*.go"},
				Cmds:    []Cmd{{Cmd: "go build"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	sources := runner.collectSources("build", make(map[string]struct{}))

	require.Len(t, sources, 1)
	assert.Equal(t, dir, sources[0].Dir)
	assert.Equal(t, []string{"*.go"}, sources[0].Patterns)
}

func TestCollectSourcesUnknownTask(t *testing.T) {
	dir := t.TempDir()
	tf := &Config{
		Dir:        dir,
		Tasks:      map[string]Task{},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	sources := runner.collectSources("missing", make(map[string]struct{}))
	assert.Empty(t, sources)
}

func TestMultiSourcesChecksumDetectsDepDirChanges(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "sub")
	writeFiles(t, dir, map[string]string{
		"main.go":    "package main",
		"sub/lib.go": "package lib",
	})

	groups := []dirPatterns{
		{Dir: dir, Patterns: []string{"*.go"}},
		{Dir: subDir, Patterns: []string{"*.go"}},
	}

	sum1, err := multiSourcesChecksum(groups)
	require.NoError(t, err)

	// Change only the file in the subdirectory
	writeFiles(t, dir, map[string]string{"sub/lib.go": "package lib // changed"})

	sum2, err := multiSourcesChecksum(groups)
	require.NoError(t, err)
	assert.NotEqual(t, sum1, sum2, "checksum should change when dep subdirectory file changes")
}

func TestWatchStopsOnContextCancel(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"main.go": "package main"})

	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Sources: StringList{"*.go"},
				Cmds:    []Cmd{{Cmd: "go build"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	captureExecs(runner)

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // cancel immediately

	err := runner.Watch(ctx, "build", "", 50*time.Millisecond)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestWatchNoSourcesInDeps(t *testing.T) {
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Deps: []Dep{{Task: "clean"}},
				Cmds: []Cmd{{Cmd: "go build"}},
			},
			"clean": {
				Cmds: []Cmd{{Cmd: "rm -rf bin"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	err := runner.Watch(t.Context(), "build", "", time.Second)
	require.EqualError(t, err, `task "build" has no sources, cannot watch`)
}

func TestWatchWritesRunErrorsToInjectedStderr(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"main.go": "package main"})
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Sources: StringList{"*.go"},
				Cmds:    []Cmd{{Cmd: "false"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	var stderr strings.Builder
	runner.IO.Stderr = &stderr

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()

	err := runner.Watch(ctx, "build", "", 50*time.Millisecond)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Contains(t, stderr.String(), `task "build"`)
}

func TestWatchDetectsEditDuringInitialBuild(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"input.txt": "original"})
	r := newTestRunner(t, &Config{Dir: dir, Tasks: map[string]Task{
		"build": {Sources: StringList{"*.txt"}, Cmds: []Cmd{{Cmd: "build"}}},
	}}, dir)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	calls := 0
	r.ShellRunner = &fakeShellRunner{runFunc: func(ShellCommand) error {
		calls++
		if calls == 1 {
			return os.WriteFile(filepath.Join(dir, "input.txt"), []byte("edited during build"), 0o644)
		}
		cancel()
		return nil
	}}
	require.ErrorIs(t, r.Watch(ctx, "build", "", minWatchInterval), context.Canceled)
	assert.Equal(t, 2, calls)
}

func TestWatchCollectsResolvedDependencySources(t *testing.T) {
	for _, dep := range []string{"api", "api:bui", "api:alias"} {
		t.Run(dep, func(t *testing.T) {
			dir := t.TempDir()
			r := newTestRunner(t, &Config{
				Dir:               dir,
				NamespaceDefaults: map[string]string{"api": "api:build"},
				Tasks: map[string]Task{
					"dev":       {Deps: []Dep{{Task: dep}}},
					"api:build": {Sources: StringList{"*.go"}, Aliases: StringList{"api:alias"}},
				},
			}, dir)
			groups := r.collectSources("dev", make(map[string]struct{}))
			require.Len(t, groups, 1)
			assert.Equal(t, []string{"*.go"}, groups[0].Patterns)
		})
	}
}

func TestWatchChecksumDetectsMovingFileBetweenGroups(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"a/input.txt": "same"})
	groups := []dirPatterns{
		{Dir: filepath.Join(dir, "a"), Patterns: []string{"*.txt"}},
		{Dir: filepath.Join(dir, "b"), Patterns: []string{"*.txt"}},
	}
	before, err := multiSourcesChecksum(groups)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "b"), 0o755))
	require.NoError(t, os.Rename(filepath.Join(dir, "a", "input.txt"), filepath.Join(dir, "b", "input.txt")))
	after, err := multiSourcesChecksum(groups)
	require.NoError(t, err)
	assert.NotEqual(t, before, after)
	unchanged, err := multiSourcesChecksum(groups)
	require.NoError(t, err)
	assert.Equal(t, after, unchanged)
}

func TestWatchDetectsAbsoluteRecursiveSources(t *testing.T) {
	dir := t.TempDir()
	sources := t.TempDir()
	writeFiles(t, sources, map[string]string{"sub/input.txt": "original"})
	r := newTestRunner(t, &Config{Dir: dir, Tasks: map[string]Task{
		"build": {Sources: StringList{filepath.Join(sources, "**", "*.txt")}, Cmds: []Cmd{{Cmd: "build"}}},
	}}, dir)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	calls := 0
	r.ShellRunner = &fakeShellRunner{runFunc: func(ShellCommand) error {
		calls++
		if calls == 1 {
			writeFiles(t, sources, map[string]string{"sub/input.txt": "changed"})
		} else {
			cancel()
		}
		return nil
	}}

	require.ErrorIs(t, r.Watch(ctx, "build", "", 10*time.Millisecond), context.Canceled)
	assert.Equal(t, 2, calls)
}
