package taskfile

import (
	"path/filepath"
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

	tf := &Taskfile{
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

	tf := &Taskfile{
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
	tf := &Taskfile{
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

func TestWatchNoSourcesInDeps(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
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
	err := runner.Watch("build", "", time.Second)
	require.EqualError(t, err, `task "build" has no sources, cannot watch`)
}
