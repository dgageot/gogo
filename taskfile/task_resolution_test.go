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

func TestPrefixMatchAcrossNamespaceSegments(t *testing.T) {
	// "gogo s:i" → "sub:install" — each colon-separated segment of the input
	// is treated as a prefix of the corresponding task-name segment, the way
	// editors filter file paths. "app:build" is excluded because its first
	// segment doesn't start with "s".
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"sub/.keep": "",
		"app/.keep": "",
	})
	subDir := filepath.Join(dir, "sub")
	appDir := filepath.Join(dir, "app")

	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"sub:install": {Dir: subDir, Cmds: []Cmd{{Cmd: "run sub-install"}}},
			"sub:build":   {Dir: subDir, Cmds: []Cmd{{Cmd: "run sub-build"}}},
			"app:build":   {Dir: appDir, Cmds: []Cmd{{Cmd: "run app-build"}}},
		},
		Namespaces: map[string]string{subDir: "sub", appDir: "app"},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("s:i", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, "sub:install", (*execs)[0].Task)
}

func TestPrefixMatchAmbiguousAcrossNamespaceSegments(t *testing.T) {
	// When the abbreviated namespace itself is ambiguous, every viable
	// target is reported and nothing runs. "s:i" matches both "sub:install"
	// and "shell:init", so resolution fails with both candidates listed.
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"sub/.keep":   "",
		"shell/.keep": "",
	})
	subDir := filepath.Join(dir, "sub")
	shellDir := filepath.Join(dir, "shell")

	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"sub:install": {Dir: subDir, Cmds: []Cmd{{Cmd: "run sub-install"}}},
			"shell:init":  {Dir: shellDir, Cmds: []Cmd{{Cmd: "run shell-init"}}},
			"sub:build":   {Dir: subDir, Cmds: []Cmd{{Cmd: "run sub-build"}}},
		},
		Namespaces: map[string]string{subDir: "sub", shellDir: "shell"},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("s:i", "")
	require.EqualError(t, err, `task "s:i" is ambiguous (matches: shell:init, sub:install)`)
	assert.Empty(t, *execs)
}

func TestPrefixMatchAcrossMultipleSegments(t *testing.T) {
	// Segment-wise matching works at arbitrary depth: "a:b:c" →
	// "app:build:compile" when that's the only triple that lines up.
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"app/build/.keep": "",
		"app/test/.keep":  "",
	})
	appBuildDir := filepath.Join(dir, "app", "build")
	appTestDir := filepath.Join(dir, "app", "test")

	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"app:build:compile": {Dir: appBuildDir, Cmds: []Cmd{{Cmd: "compile"}}},
			"app:test:run":      {Dir: appTestDir, Cmds: []Cmd{{Cmd: "test-run"}}},
		},
		Namespaces: map[string]string{appBuildDir: "app:build", appTestDir: "app:test"},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("a:b:c", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, "app:build:compile", (*execs)[0].Task)
}

func TestPrefixMatchSegmentCountMustAgree(t *testing.T) {
	// A bare "sub" no longer matches "sub:install" — segment-wise matching
	// requires the user to type something for every namespace level they
	// want to span. Mistyping the namespace as if it were a flat task name
	// now fails fast rather than ambiguously matching every task under it.
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"sub/.keep": ""})
	subDir := filepath.Join(dir, "sub")

	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"sub:install": {Dir: subDir, Cmds: []Cmd{{Cmd: "run sub-install"}}},
			"sub:build":   {Dir: subDir, Cmds: []Cmd{{Cmd: "run sub-build"}}},
		},
		Namespaces: map[string]string{subDir: "sub"},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("sub", "")
	require.EqualError(t, err, `task "sub" not found`)
	assert.Empty(t, *execs)
}

func TestPrefixMatchResolvesViaAlias(t *testing.T) {
	// A unique alias prefix resolves to the aliased task even though
	// the task's own name starts with a different letter.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"github": {
				Aliases: StringList{"gh"},
				Cmds:    []Cmd{{Cmd: "gh auth status"}},
			},
			"build": {Cmds: []Cmd{{Cmd: "go build"}}},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("g", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, "github", (*execs)[0].Task)
}

func TestPrefixMatchResolvesViaAliasOnly(t *testing.T) {
	// The task name "generate-protos" does not start with "g" in a
	// prefix-unique way because "github" also starts with "g". But the
	// alias "gp" is unique so "gp" must resolve via the alias.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"generate-protos": {
				Aliases: StringList{"gp"},
				Cmds:    []Cmd{{Cmd: "protoc"}},
			},
			"github": {Cmds: []Cmd{{Cmd: "gh auth status"}}},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("gp", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, "generate-protos", (*execs)[0].Task)
}

func TestPrefixMatchAmbiguousWithAlias(t *testing.T) {
	// "g" matches both the task name "github" and the alias "gp" (which
	// points to "generate-protos"), creating ambiguity.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"generate-protos": {
				Aliases: StringList{"gp"},
				Cmds:    []Cmd{{Cmd: "protoc"}},
			},
			"github": {Cmds: []Cmd{{Cmd: "gh auth status"}}},
			"build":  {Cmds: []Cmd{{Cmd: "go build"}}},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("g", "")
	require.EqualError(t, err, `task "g" is ambiguous (matches: generate-protos, github)`)
	assert.Empty(t, *execs)
}

func TestPrefixMatchAliasAndTaskNameDedup(t *testing.T) {
	// When both the task name and an alias of the same task match, the
	// task should appear only once in the results.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"install": {
				Aliases: StringList{"inst"},
				Cmds:    []Cmd{{Cmd: "make install"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	// "ins" is a prefix of both "install" (task name) and "inst" (alias)
	// but should resolve unambiguously.
	require.NoError(t, runner.Run("ins", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, "install", (*execs)[0].Task)
}

func TestPrefixMatchAliasSkipsInternalTask(t *testing.T) {
	// An alias pointing to an internal task must be excluded from prefix
	// matching, the same way internal task names are excluded.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"_setup": {
				Aliases: StringList{"su"},
				Cmds:    []Cmd{{Cmd: "setup"}},
			},
			"serve": {Cmds: []Cmd{{Cmd: "serve"}}},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	// "s" should uniquely match "serve" — the alias "su" -> "_setup" is
	// internal and must not participate.
	require.NoError(t, runner.Run("s", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, "serve", (*execs)[0].Task)
}

func TestPrefixMatchNamespacedAlias(t *testing.T) {
	// Aliases work with namespaced prefix matching too:
	// "sub:g" matches alias "sub:gp" → task "sub:generate-protos".
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"sub/.keep": ""})
	subDir := filepath.Join(dir, "sub")

	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"sub:generate-protos": {
				Dir:     subDir,
				Aliases: StringList{"sub:gp"},
				Cmds:    []Cmd{{Cmd: "protoc"}},
			},
			"sub:build": {
				Dir:  subDir,
				Cmds: []Cmd{{Cmd: "go build"}},
			},
		},
		Namespaces: map[string]string{subDir: "sub"},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("sub:g", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, "sub:generate-protos", (*execs)[0].Task)
}

func TestPrefixMatchLeadingColon(t *testing.T) {
	// A leading colon should not match anything — it produces an empty
	// first segment which can never prefix-match a real task name.
	dir := t.TempDir()
	tf := makePrefixTF(dir, "install", "build")
	runner := newTestRunner(t, tf, dir)

	err := runner.Run(":install", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}
