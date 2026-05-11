package taskfile

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makePrefixTF builds a Config with the given top-level task names. Each task
// runs a no-op `cmd: true` so the runner can execute it without spawning a
// shell (the fake shell runner intercepts).
func makePrefixTF(dir string, names ...string) *Config {
	tasks := make(map[string]Task, len(names))
	for _, n := range names {
		tasks[n] = Task{Cmds: []Cmd{{Cmd: "run-" + n}}}
	}
	return &Config{
		Dir:        dir,
		Tasks:      tasks,
		DotenvVars: make(map[string]string),
	}
}

func TestPrefixMatchUniqueResolvesToFullName(t *testing.T) {
	// "gogo i" → "install" because "install" is the only task whose name
	// starts with "i".
	dir := t.TempDir()
	tf := makePrefixTF(dir, "install", "build", "test")

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("i", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, "install", (*execs)[0].Task)
}

func TestPrefixMatchLongerSubstring(t *testing.T) {
	// "gogo inst" still resolves to "install".
	dir := t.TempDir()
	tf := makePrefixTF(dir, "install", "build")

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("inst", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, "install", (*execs)[0].Task)
}

func TestPrefixMatchAmbiguousIsRejected(t *testing.T) {
	// Two tasks start with "i": resolution must fail and *neither* should
	// run. The error lists the candidates sorted for stable output.
	dir := t.TempDir()
	tf := makePrefixTF(dir, "install", "info", "build")

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("i", "")
	require.EqualError(t, err, `task "i" is ambiguous (matches: info, install)`)
	assert.Empty(t, *execs)
}

func TestPrefixMatchExactWinsOverPrefix(t *testing.T) {
	// When a task literally named "i" exists, prefix matching never kicks in
	// even though "install" also starts with "i". Exact resolution is final.
	dir := t.TempDir()
	tf := makePrefixTF(dir, "i", "install")

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("i", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, "i", (*execs)[0].Task)
}

func TestPrefixMatchSkipsInternalTasks(t *testing.T) {
	// "_init" is an internal task and must not participate in prefix matching:
	// "gogo i" still uniquely resolves to "install".
	dir := t.TempDir()
	tf := makePrefixTF(dir, "install", "_init")

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("i", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, "install", (*execs)[0].Task)
}

func TestPrefixMatchNoMatchReportsNotFound(t *testing.T) {
	// An unmatchable prefix still reports the familiar "task not found"
	// error so we don't change the error surface for genuine typos.
	dir := t.TempDir()
	tf := makePrefixTF(dir, "build", "test")

	runner := newTestRunner(t, tf, dir)
	captureExecs(runner)

	err := runner.Run("xyz", "")
	assert.EqualError(t, err, `task "xyz" not found`)
}

func TestPrefixMatchWithNamespacedInput(t *testing.T) {
	// "gogo sub:i" → "sub:install" when "sub:install" is the only task that
	// begins with "sub:i".
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"sub/.keep": ""})
	subDir := filepath.Join(dir, "sub")

	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"sub:install": {Dir: subDir, Cmds: []Cmd{{Cmd: "run sub-install"}}},
			"sub:build":   {Dir: subDir, Cmds: []Cmd{{Cmd: "run sub-build"}}},
			"install":     {Cmds: []Cmd{{Cmd: "run-install"}}},
		},
		Namespaces: map[string]string{subDir: "sub"},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("sub:i", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, "sub:install", (*execs)[0].Task)
}

func TestPrefixMatchAmbiguousWithinNamespace(t *testing.T) {
	// Ambiguity check applies per-namespace too: "sub:i" matches both
	// "sub:install" and "sub:info", so neither should run.
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"sub/.keep": ""})
	subDir := filepath.Join(dir, "sub")

	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"sub:install": {Dir: subDir, Cmds: []Cmd{{Cmd: "run sub-install"}}},
			"sub:info":    {Dir: subDir, Cmds: []Cmd{{Cmd: "run sub-info"}}},
		},
		Namespaces: map[string]string{subDir: "sub"},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("sub:i", "")
	require.EqualError(t, err, `task "sub:i" is ambiguous (matches: sub:info, sub:install)`)
	assert.Empty(t, *execs)
}

func TestPrefixMatchFromCwdNamespace(t *testing.T) {
	// When cwd is inside the "sub" include and the bare name has no
	// top-level match, the cwd namespace is consulted before failing.
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"sub/.keep": ""})
	subDir := filepath.Join(dir, "sub")

	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"sub:install": {Dir: subDir, Cmds: []Cmd{{Cmd: "run sub-install"}}},
			"build":       {Cmds: []Cmd{{Cmd: "run-build"}}},
		},
		Namespaces: map[string]string{subDir: "sub"},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, subDir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("i", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, "sub:install", (*execs)[0].Task)
}

func TestPrefixMatchPrefersTopLevelOverCwdNamespace(t *testing.T) {
	// Top-level prefix matches take precedence over cwd-namespaced ones,
	// mirroring how exact resolution works in resolveTaskName.
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"sub/.keep": ""})
	subDir := filepath.Join(dir, "sub")

	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"install":     {Cmds: []Cmd{{Cmd: "run-install"}}},
			"sub:install": {Dir: subDir, Cmds: []Cmd{{Cmd: "run sub-install"}}},
		},
		Namespaces: map[string]string{subDir: "sub"},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, subDir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("i", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, "install", (*execs)[0].Task)
}

func TestPrefixMatchSelfPrefix(t *testing.T) {
	// When running a sub task file as its own root, "proxy:i" resolves to
	// "install" the same way "proxy:install" does — the self-prefix is
	// stripped before prefix matching.
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"proxy/.keep": ""})
	proxyDir := filepath.Join(dir, "proxy")

	tf := &Config{
		Dir: proxyDir,
		Tasks: map[string]Task{
			"install": {Cmds: []Cmd{{Cmd: "run-install"}}},
			"build":   {Cmds: []Cmd{{Cmd: "run-build"}}},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, proxyDir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("proxy:i", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, "install", (*execs)[0].Task)
}

func TestPrefixMatchHonoursTopLevelDefault(t *testing.T) {
	// The top-level `default:` field is validated at load time and must
	// point at an exact task name — prefix matching is *not* applied there.
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
default: ins
tasks:
  install:
    cmd: true
`,
	})

	_, err := LoadWithIncludes(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"ins"`)
}

func TestPrefixMatchEmptyName(t *testing.T) {
	dir := t.TempDir()
	tf := makePrefixTF(dir, "install", "build")
	runner := newTestRunner(t, tf, dir)

	err := runner.Run("", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestPrefixMatchColonOnly(t *testing.T) {
	dir := t.TempDir()
	tf := makePrefixTF(dir, "install", "build")
	runner := newTestRunner(t, tf, dir)

	err := runner.Run(":", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestPrefixMatchTrailingColon(t *testing.T) {
	dir := t.TempDir()
	tf := makePrefixTF(dir, "install", "build")
	runner := newTestRunner(t, tf, dir)

	err := runner.Run("task:", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestPrefixMatchWhenExactMatchIsAlsoPrefix(t *testing.T) {
	// If we have tasks "i" and "install", and user types "i",
	// exact match should win (return "i"), not prefix match (which would be ambiguous).
	dir := t.TempDir()
	tf := makePrefixTF(dir, "i", "install")
	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("i", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, "i", (*execs)[0].Task)
}

func TestIsInternalTaskEdgeCases(t *testing.T) {
	assert.True(t, IsInternalTask("_"))
	assert.True(t, IsInternalTask("_a"))
	assert.True(t, IsInternalTask("ns:_"))
	assert.True(t, IsInternalTask("ns:_a"))
	assert.False(t, IsInternalTask("a_b"))
	assert.False(t, IsInternalTask("ns:a_b"))
	assert.False(t, IsInternalTask(""))
	assert.False(t, IsInternalTask(":"))
	assert.False(t, IsInternalTask("ns:"))
}
