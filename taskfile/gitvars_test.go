package taskfile

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeGit returns a *fakeShellRunner whose Output stub maps known git
// commands to canned outputs and returns an error for anything else
// (mirroring how `git rev-parse HEAD` would behave outside a repo). The
// returned counter records how many times each command was invoked, so
// tests can assert that memoization is working.
func fakeGit(values map[string]string) (*fakeShellRunner, *atomic.Int64) {
	var calls atomic.Int64
	shell := &fakeShellRunner{
		outputFunc: func(req ShellCommand) ([]byte, error) {
			calls.Add(1)
			if v, ok := values[req.Command]; ok {
				return []byte(v + "\n"), nil
			}
			return nil, errors.New("not in a git repo")
		},
	}
	return shell, &calls
}

func TestGitVarsLookupKnownNames(t *testing.T) {
	sh, _ := fakeGit(map[string]string{
		"git rev-parse HEAD":                                                     "abc1234567890",
		"git rev-parse --short=7 HEAD":                                           "abc1234",
		"git describe --tags --exact-match HEAD 2>/dev/null":                     "v1.2.3",
		"git rev-parse --abbrev-ref HEAD":                                        "main",
		`if [ -n "$(git status --porcelain 2>/dev/null)" ]; then echo dirty; fi`: "dirty",
	})
	g := newGitVars("/somewhere", sh)

	for name, want := range map[string]string{
		"GIT_COMMIT":       "abc1234567890",
		"GIT_SHORT_COMMIT": "abc1234",
		"GIT_TAG":          "v1.2.3",
		"GIT_BRANCH":       "main",
		"GIT_DIRTY":        "dirty",
	} {
		got, ok := g.lookup(name)
		assert.True(t, ok, "lookup(%q) should report known", name)
		assert.Equal(t, want, got, "lookup(%q)", name)
	}
}

func TestGitVarsLookupUnknownReturnsFalse(t *testing.T) {
	sh, _ := fakeGit(nil)
	g := newGitVars("", sh)

	val, ok := g.lookup("NOT_A_BUILTIN")
	assert.False(t, ok)
	assert.Empty(t, val)
}

func TestGitVarsLookupNilReceiverIsSafe(t *testing.T) {
	// builtinLookup constructs gitVars lazily; callers should never see a
	// non-nil panic if they happen to call lookup before construction.
	var g *gitVars
	val, ok := g.lookup("GIT_COMMIT")
	assert.False(t, ok)
	assert.Empty(t, val)
}

func TestGitVarsErrorBecomesEmptyValue(t *testing.T) {
	// Outside a git repo every command exits non-zero; gogo squashes that to
	// an empty string and still reports the name as known so that
	// `requires.vars: [GIT_COMMIT]` doesn't fail when the user is just
	// checking it's referenced.
	sh, _ := fakeGit(nil) // every command fails
	g := newGitVars("", sh)

	for _, name := range builtinGitVars {
		val, ok := g.lookup(name)
		assert.True(t, ok, "%s should still be reported as known", name)
		assert.Empty(t, val, "%s should resolve to empty on error", name)
	}
}

func TestGitVarsMemoizesAcrossCalls(t *testing.T) {
	sh, calls := fakeGit(map[string]string{"git rev-parse HEAD": "deadbeef"})
	g := newGitVars("", sh)

	for range 5 {
		val, _ := g.lookup("GIT_COMMIT")
		assert.Equal(t, "deadbeef", val)
	}
	assert.Equal(t, int64(1), calls.Load(), "git should be invoked exactly once")
}

func TestRunnerBuiltinLookupExposesGitVars(t *testing.T) {
	sh, _ := fakeGit(map[string]string{"git rev-parse HEAD": "feedface"})
	r := newTestRunner(t, &Config{}, t.TempDir())
	r.ShellRunner = sh

	val, ok := r.builtinLookup("GIT_COMMIT")
	assert.True(t, ok)
	assert.Equal(t, "feedface", val)
}

func TestExpandVarsResolvesBuiltinTemplate(t *testing.T) {
	builtin := func(name string) (string, bool) {
		if name == "GIT_COMMIT" {
			return "abc1234", true
		}
		return "", false
	}
	got := expandVars("commit={{.GIT_COMMIT}}", nil, "", builtin)
	assert.Equal(t, "commit=abc1234", got)
}

func TestExpandVarsResolvesBuiltinShellSyntax(t *testing.T) {
	builtin := func(name string) (string, bool) {
		if name == "GIT_BRANCH" {
			return "main", true
		}
		return "", false
	}
	got := expandVars("branch=${GIT_BRANCH}", nil, "", builtin)
	assert.Equal(t, "branch=main", got)
}

func TestExpandVarsUserVarOverridesBuiltin(t *testing.T) {
	// User-defined vars must always win over built-ins so a project can
	// pin GIT_COMMIT in CI to a specific value (e.g. for reproducible builds).
	builtin := func(name string) (string, bool) {
		if name == "GIT_COMMIT" {
			return "from-git", true
		}
		return "", false
	}
	vars := map[string]string{"GIT_COMMIT": "user-pinned"}
	got := expandVars("{{.GIT_COMMIT}}", vars, "", builtin)
	assert.Equal(t, "user-pinned", got)
}

func TestExpandVarsBuiltinBeatsEnv(t *testing.T) {
	t.Setenv("GIT_COMMIT", "env-value")
	builtin := func(name string) (string, bool) {
		if name == "GIT_COMMIT" {
			return "from-git", true
		}
		return "", false
	}
	got := expandVars("{{.GIT_COMMIT}}", nil, "", builtin)
	assert.Equal(t, "from-git", got, "builtin lookup wins over OS env")
}

func TestExpandVarsNilBuiltinIsNoop(t *testing.T) {
	got := expandVars("{{.GIT_COMMIT}}", nil, "", nil)
	assert.Equal(t, "{{.GIT_COMMIT}}", got, "unknown templates left verbatim")
}

func TestResolveEnvValueConsultsBuiltin(t *testing.T) {
	builtin := func(name string) (string, bool) {
		if name == "GIT_TAG" {
			return "v1.2.3", true
		}
		return "", false
	}
	taskEnv := map[string]string{"VERSION": "${GIT_TAG}"}
	got := resolveEnvValue("VERSION", taskEnv, nil, builtin)
	assert.Equal(t, "v1.2.3", got)
}

func TestRunnerCmdSeesBuiltinGitVars(t *testing.T) {
	// End-to-end: a task uses {{.GIT_COMMIT}} in its cmd; the expanded
	// command reaching the shell runner must contain the resolved SHA.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {Cmds: []Cmd{{Cmd: "echo {{.GIT_COMMIT}}"}}},
		},
	}
	sh, _ := fakeGit(map[string]string{"git rev-parse HEAD": "cafef00d"})
	r := newTestRunner(t, tf, dir)
	r.ShellRunner = sh

	require.NoError(t, r.Run("build", ""))

	runs := sh.runsSnapshot()
	taskRuns := runs[:0]
	for _, run := range runs {
		if run.Kind == ShellCommandTask {
			taskRuns = append(taskRuns, run)
		}
	}
	require.Len(t, taskRuns, 1)
	assert.Equal(t, "echo cafef00d", taskRuns[0].Command)
}

func TestRunnerCmdShellSyntaxBuiltinExpansion(t *testing.T) {
	// The ${GIT_BRANCH} form is resolved by gogo (via os.Expand in
	// expandVars) before the command reaches the shell, so it works even
	// when the actual shell environment doesn't carry GIT_BRANCH.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"info": {Cmds: []Cmd{{Cmd: "echo ${GIT_BRANCH}"}}},
		},
	}
	sh, _ := fakeGit(map[string]string{"git rev-parse --abbrev-ref HEAD": "feature/x"})
	r := newTestRunner(t, tf, dir)
	r.ShellRunner = sh

	require.NoError(t, r.Run("info", ""))

	for _, run := range sh.runsSnapshot() {
		if run.Kind == ShellCommandTask {
			assert.Equal(t, "echo feature/x", run.Command)
		}
	}
}

func TestRunnerEnvBlockSeesBuiltinGitVars(t *testing.T) {
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Cmds: []Cmd{{Cmd: "true"}},
				Env:  map[string]string{"VERSION": "${GIT_TAG}"},
			},
		},
	}
	sh, _ := fakeGit(map[string]string{
		"git describe --tags --exact-match HEAD 2>/dev/null": "v9.9.9",
	})
	r := newTestRunner(t, tf, dir)
	r.ShellRunner = sh

	require.NoError(t, r.Run("build", ""))

	for _, run := range sh.runsSnapshot() {
		if run.Kind == ShellCommandTask {
			assert.Contains(t, run.Env, "VERSION=v9.9.9")
		}
	}
}

func TestRunnerRequiresAcceptsBuiltinGitVar(t *testing.T) {
	// `requires.vars: [GIT_COMMIT]` is satisfied by the built-in even in a
	// directory without an explicit `vars:` block, as long as the built-in
	// is *known* (the empty string is a valid value).
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Cmds:     []Cmd{{Cmd: "true"}},
				Requires: Requires{Vars: []string{"GIT_COMMIT"}},
			},
		},
	}
	sh, _ := fakeGit(nil) // simulate "outside a repo": git fails, value is empty
	r := newTestRunner(t, tf, dir)
	r.ShellRunner = sh

	require.NoError(t, r.Run("build", ""))
}

func TestRunnerUserVarOverridesBuiltinGitVar(t *testing.T) {
	// Users must always be able to pin a built-in name from `vars:`.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Vars: map[string]Var{
			"GIT_COMMIT": {Value: "user-pinned"},
		},
		Tasks: map[string]Task{
			"build": {Cmds: []Cmd{{Cmd: "echo {{.GIT_COMMIT}}"}}},
		},
	}
	sh, _ := fakeGit(map[string]string{"git rev-parse HEAD": "would-be-from-git"})
	r := newTestRunner(t, tf, dir)
	r.ShellRunner = sh

	require.NoError(t, r.Run("build", ""))

	for _, run := range sh.runsSnapshot() {
		if run.Kind == ShellCommandTask {
			assert.Equal(t, "echo user-pinned", run.Command)
		}
	}
}

func TestRunnerBuiltinNotInvokedWhenNotReferenced(t *testing.T) {
	// Built-ins are lazy: a task that doesn't reference any GIT_* name must
	// not trigger a single git invocation. This is what makes the feature
	// affordable to enable by default.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"clean": {Cmds: []Cmd{{Cmd: "rm -rf bin"}}},
		},
	}
	sh, calls := fakeGit(map[string]string{
		"git rev-parse HEAD": "should-never-be-called",
	})
	r := newTestRunner(t, tf, dir)
	r.ShellRunner = sh

	require.NoError(t, r.Run("clean", ""))

	// Sanity: count only Output() invocations (the var path), not Run().
	outputs := sh.outputsSnapshot()
	assert.Empty(t, outputs, "no `vars:` resolution should fire for an unrelated task")
	assert.Equal(t, int64(0), calls.Load())
}

func TestRunnerBuiltinInvokedOncePerRunnerAcrossTasks(t *testing.T) {
	// Even when several tasks reference the same built-in, gogo asks git
	// only once per Runner thanks to OnceValue caching.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"a": {Cmds: []Cmd{{Cmd: "echo {{.GIT_COMMIT}}"}}},
			"b": {Cmds: []Cmd{{Cmd: "echo {{.GIT_COMMIT}}"}}},
			"c": {Cmds: []Cmd{{Cmd: "echo {{.GIT_COMMIT}}"}}},
		},
	}
	sh, _ := fakeGit(map[string]string{"git rev-parse HEAD": "abc"})
	r := newTestRunner(t, tf, dir)
	r.ShellRunner = sh

	for _, name := range []string{"a", "b", "c"} {
		r.ResetRan()
		// ResetRan also clears the gitVars cache between iterations, so for
		// this assertion run them without resetting between calls.
		_ = name
	}
	r = newTestRunner(t, tf, dir)
	r.ShellRunner = sh
	require.NoError(t, r.Run("a", ""))
	require.NoError(t, r.Run("b", ""))
	require.NoError(t, r.Run("c", ""))

	// Count git rev-parse HEAD invocations on this fakeShellRunner.
	var rev int
	for _, out := range sh.outputsSnapshot() {
		if strings.Contains(out.Command, "git rev-parse HEAD") {
			rev++
		}
	}
	assert.Equal(t, 1, rev, "git rev-parse HEAD must be memoized across tasks of the same Runner")
}

func TestResetRanReevaluatesBuiltinGitVars(t *testing.T) {
	// In watch mode, ResetRan is called between iterations. The dirty state
	// (and tag/branch on long-lived sessions) may have changed since the
	// last evaluation, so the cache must be invalidated.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"info": {Cmds: []Cmd{{Cmd: "echo {{.GIT_DIRTY}}"}}},
		},
	}
	values := map[string]string{
		`if [ -n "$(git status --porcelain 2>/dev/null)" ]; then echo dirty; fi`: "",
	}
	sh := &fakeShellRunner{
		outputFunc: func(req ShellCommand) ([]byte, error) {
			if v, ok := values[req.Command]; ok {
				return []byte(v + "\n"), nil
			}
			return nil, errors.New("unexpected: " + req.Command)
		},
	}
	r := newTestRunner(t, tf, dir)
	r.ShellRunner = sh

	require.NoError(t, r.Run("info", ""))

	// Simulate an edit between watch iterations: the working tree is now
	// dirty. Without ResetRan invalidating the gitVars cache, the second
	// iteration would still report empty.
	values[`if [ -n "$(git status --porcelain 2>/dev/null)" ]; then echo dirty; fi`] = "dirty"
	r.ResetRan()
	require.NoError(t, r.Run("info", ""))

	taskRuns := []ShellCommand{}
	for _, run := range sh.runsSnapshot() {
		if run.Kind == ShellCommandTask {
			taskRuns = append(taskRuns, run)
		}
	}
	require.Len(t, taskRuns, 2)
	assert.Equal(t, "echo ", taskRuns[0].Command, "first iteration: clean tree")
	assert.Equal(t, "echo dirty", taskRuns[1].Command, "second iteration: dirty tree")
}
