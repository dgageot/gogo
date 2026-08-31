package taskfile

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func patternTaskNames(execs []Execution) []string {
	names := make([]string, len(execs))
	for i, e := range execs {
		names[i] = e.Task
	}
	return names
}

func TestPatternRunsTaskAcrossNamespaces(t *testing.T) {
	dir := t.TempDir()

	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"test":       {Cmds: []Cmd{{Cmd: "run root-test"}}},
			"proxy:test": {Cmds: []Cmd{{Cmd: "run proxy-test"}}},
			"a:b:test":   {Cmds: []Cmd{{Cmd: "run nested-test"}}},
			"proxy:lint": {Cmds: []Cmd{{Cmd: "run proxy-lint"}}},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("...:test", ""))
	assert.ElementsMatch(t, []string{"test", "proxy:test", "a:b:test"}, patternTaskNames(*execs))
}

func TestPatternSkipsInternalTasks(t *testing.T) {
	dir := t.TempDir()

	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"proxy:_helper": {Cmds: []Cmd{{Cmd: "run helper"}}},
			"proxy:test":    {Cmds: []Cmd{{Cmd: "run proxy-test"}}},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("...:_helper", "")
	require.EqualError(t, err, `no tasks match pattern "...:_helper"`)
	assert.Empty(t, *execs)
}

func TestPatternWithoutMatchIsAnError(t *testing.T) {
	dir := t.TempDir()

	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"proxy:build": {Cmds: []Cmd{{Cmd: "run proxy-build"}}},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)

	err := runner.Run("...:test", "")
	require.EqualError(t, err, `no tasks match pattern "...:test"`)
}

func TestPatternDoesNotPrefixMatch(t *testing.T) {
	// Wildcards demand an exact final segment: `...:test` must not fuzz to
	// `proxy:testing` the way plain prefix resolution would.
	dir := t.TempDir()

	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"proxy:testing": {Cmds: []Cmd{{Cmd: "run proxy-testing"}}},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)

	err := runner.Run("...:test", "")
	require.EqualError(t, err, `no tasks match pattern "...:test"`)
}

func TestPatternScopedToCwdNamespace(t *testing.T) {
	// Running `gogo ...:test` from inside an include only spans that
	// subtree, mirroring Bazel's cwd-relative `...` patterns.
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"sub/.keep": ""})
	subDir := filepath.Join(dir, "sub")

	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"test":           {Cmds: []Cmd{{Cmd: "run root-test"}}},
			"sub:test":       {Dir: subDir, Cmds: []Cmd{{Cmd: "run sub-test"}}},
			"sub:inner:test": {Dir: subDir, Cmds: []Cmd{{Cmd: "run sub-inner-test"}}},
		},
		Namespaces: map[string]string{subDir: "sub"},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, subDir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("...:test", ""))
	assert.ElementsMatch(t, []string{"sub:test", "sub:inner:test"}, patternTaskNames(*execs))
}

func TestPatternKeepsGoingOnFailure(t *testing.T) {
	// One broken namespace must not hide the others: every match runs and
	// the failures are aggregated.
	dir := t.TempDir()

	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"bad:test":  {Cmds: []Cmd{{Cmd: "false"}}},
			"good:test": {Cmds: []Cmd{{Cmd: "run good-test"}}},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("...:test", "")
	require.ErrorContains(t, err, `task "bad:test"`)
	assert.ElementsMatch(t, []string{"bad:test", "good:test"}, patternTaskNames(*execs))
}

func TestPatternEmptySuffixIsAnError(t *testing.T) {
	dir := t.TempDir()

	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"proxy:test": {Cmds: []Cmd{{Cmd: "run proxy-test"}}},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)

	err := runner.Run("...:", "")
	require.EqualError(t, err, `invalid pattern "...:": expected "...:<task>"`)
}

func TestPatternAsDependency(t *testing.T) {
	dir := t.TempDir()

	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"all":      {Deps: []Dep{{Task: "...:test"}}},
			"a:test":   {Cmds: []Cmd{{Cmd: "run a-test"}}},
			"b:test":   {Cmds: []Cmd{{Cmd: "run b-test"}}},
			"b:deploy": {Cmds: []Cmd{{Cmd: "run b-deploy"}}},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("all", ""))
	assert.ElementsMatch(t, []string{"a:test", "b:test"}, patternTaskNames(*execs))
}

func TestPatternAsSubTaskCallForwardsVars(t *testing.T) {
	// `task: ...:X` sub-calls fan out to every match, and call-site vars
	// reach each of them.
	dir := t.TempDir()

	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"all": {Cmds: []Cmd{
				{Task: "...:greet", Vars: map[string]Var{"MSG": {Value: "hello"}}},
			}},
			"a:greet": {Cmds: []Cmd{{Cmd: "echo a {{.MSG}}"}}},
			"b:greet": {Cmds: []Cmd{{Cmd: "echo b {{.MSG}}"}}},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("all", ""))
	require.Len(t, *execs, 2)
	assert.Equal(t, "echo a hello", (*execs)[0].Command)
	assert.Equal(t, "echo b hello", (*execs)[1].Command)
}

func TestPatternIsRejectedByWatch(t *testing.T) {
	dir := t.TempDir()

	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"a:test": {Cmds: []Cmd{{Cmd: "run a-test"}}, Sources: StringList{"*.go"}},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)

	err := runner.Watch(t.Context(), "...:test", "", time.Second)
	require.EqualError(t, err, `patterns like "...:test" are not supported with --watch`)
}

func TestPatternDepContributesWatchSources(t *testing.T) {
	// Watching a task whose dep is a pattern must poll the matched tasks'
	// sources, or edits to them would never trigger a re-run.
	dir := t.TempDir()

	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"all":    {Deps: []Dep{{Task: "...:test"}}},
			"a:test": {Cmds: []Cmd{{Cmd: "run a-test"}}, Sources: StringList{"a/*.go"}},
			"b:test": {Cmds: []Cmd{{Cmd: "run b-test"}}, Sources: StringList{"b/*.go"}},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)

	sources := runner.collectSources("all", make(map[string]struct{}))
	var patterns []string
	for _, s := range sources {
		patterns = append(patterns, s.Patterns...)
	}
	assert.ElementsMatch(t, []string{"a/*.go", "b/*.go"}, patterns)
}

func TestPatternPromptsAreSerialized(t *testing.T) {
	// Two prompt-guarded matches run concurrently; each must consume exactly
	// one answer line from the shared stdin, so "y\ny\n" confirms both.
	dir := t.TempDir()

	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"a:deploy": {Prompt: "deploy a?", Cmds: []Cmd{{Cmd: "run a-deploy"}}},
			"b:deploy": {Prompt: "deploy b?", Cmds: []Cmd{{Cmd: "run b-deploy"}}},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	runner.IO.Stdin = strings.NewReader("y\ny\n") //nolint:dupword // duplication is expected
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("...:deploy", ""))
	assert.ElementsMatch(t, []string{"a:deploy", "b:deploy"}, patternTaskNames(*execs))
}
