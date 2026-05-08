package taskfile

import (
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Top-level `default:` field ---------------------------------------------

func TestParseTopLevelDefaultField(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gogo.yaml"), []byte(`version: "1"
default: build
tasks:
  build:
    cmd: go build .
`), 0o644))

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)
	assert.Equal(t, "build", tf.Default)
}

func TestLoadWithIncludesRejectsUnknownDefault(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gogo.yaml"), []byte(`version: "1"
default: missing
tasks:
  build:
    cmd: go build .
`), 0o644))

	_, err := LoadWithIncludes(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"missing"`)
	assert.Contains(t, err.Error(), "default:")
}

func TestLoadWithIncludesAcceptsDefaultPointingToIncludedTask(t *testing.T) {
	// The validator runs after includes are merged so a top-level
	// `default:` may reference a namespaced task pulled in via `includes:`.
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
default: cli:build
includes:
  - cli
`,
		"cli/gogo.yaml": `version: "1"
tasks:
  build:
    cmd: echo cli build
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)
	assert.Equal(t, "cli:build", tf.Default)
	assert.Contains(t, tf.Tasks, "cli:build")
}

func TestDefaultFieldRoundtripsThroughLoad(t *testing.T) {
	// Smoke test: top-level `default:` survives parsing and (when valid)
	// makes it through include resolution to the final Config.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gogo.yaml"), []byte(`version: "1"
default: dev
tasks:
  dev:
    cmd: echo dev
`), 0o644))

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)
	assert.Equal(t, "dev", tf.Default)
}

// --- Env propagation --------------------------------------------------------

func TestSubTaskInheritsParentEnv(t *testing.T) {
	// The classic case from ai/evals/gogo.yaml: a parent `env:` block must
	// reach the sub-task without the user having to shell out to `gogo`.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"parent": {
				Env:  map[string]string{"TESTSET_SIZE": "2", "MIRROR_FS": "1"},
				Cmds: []Cmd{{Task: "child"}},
			},
			"child": {
				Cmds: []Cmd{{Cmd: "true"}},
			},
		},
	}
	r := newTestRunner(t, tf, dir)
	execs := captureExecs(r)

	require.NoError(t, r.Run("parent", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, "child", (*execs)[0].Task)
	assert.Equal(t, "2", envValue((*execs)[0].Env, "TESTSET_SIZE"))
	assert.Equal(t, "1", envValue((*execs)[0].Env, "MIRROR_FS"))
}

func TestSubTaskOwnEnvOverridesParent(t *testing.T) {
	// Per-key precedence: when the child also declares an env entry, the
	// child's value wins. This is the same rule that applies to BaseEnv vs
	// task.env, just one level up the call stack.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"parent": {
				Env:  map[string]string{"MODE": "from-parent", "SHARED": "parent-only"},
				Cmds: []Cmd{{Task: "child"}},
			},
			"child": {
				Env:  map[string]string{"MODE": "from-child"},
				Cmds: []Cmd{{Cmd: "true"}},
			},
		},
	}
	r := newTestRunner(t, tf, dir)
	execs := captureExecs(r)

	require.NoError(t, r.Run("parent", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, "from-child", envValue((*execs)[0].Env, "MODE"), "child env wins per-key")
	assert.Equal(t, "parent-only", envValue((*execs)[0].Env, "SHARED"), "non-overlapping parent keys still flow through")
}

func TestSubTaskEnvVisibleInCmdExpansion(t *testing.T) {
	// End-to-end: parent env propagates down so the shell that runs the
	// child cmd has the parent's env in its environment. gogo itself does
	// not pre-expand ${VAR} from env (that's the shell's job at runtime),
	// so we assert on the child's Env slice rather than the rendered cmd.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"parent": {
				Env:  map[string]string{"GREETING": "hello"},
				Cmds: []Cmd{{Task: "child"}},
			},
			"child": {
				Cmds: []Cmd{{Cmd: "echo ${GREETING}"}},
			},
		},
	}
	r := newTestRunner(t, tf, dir)
	execs := captureExecs(r)

	require.NoError(t, r.Run("parent", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, "echo ${GREETING}", (*execs)[0].Command, "gogo leaves ${VAR} from env for the shell")
	assert.Equal(t, "hello", envValue((*execs)[0].Env, "GREETING"), "and the env reaches the shell")
}

func TestSubTaskInheritanceIsTransitive(t *testing.T) {
	// parent -> mid -> leaf: the leaf must see env declared all the way up
	// the chain. Each level may layer additional env on top.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"parent": {
				Env:  map[string]string{"FROM_PARENT": "P"},
				Cmds: []Cmd{{Task: "mid"}},
			},
			"mid": {
				Env:  map[string]string{"FROM_MID": "M"},
				Cmds: []Cmd{{Task: "leaf"}},
			},
			"leaf": {
				Cmds: []Cmd{{Cmd: "true"}},
			},
		},
	}
	r := newTestRunner(t, tf, dir)
	execs := captureExecs(r)

	require.NoError(t, r.Run("parent", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, "P", envValue((*execs)[0].Env, "FROM_PARENT"))
	assert.Equal(t, "M", envValue((*execs)[0].Env, "FROM_MID"))
}

func TestDepsDoNotInheritParentEnv(t *testing.T) {
	// Deps run before the parent's body and are conceptually prerequisites,
	// not sequenced sub-calls. They must NOT see parent.env, so a parent's
	// "set up the env, then run me" workflow stays predictable.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"parent": {
				Env:  map[string]string{"PARENT_ONLY": "yes"},
				Deps: []Dep{{Task: "prereq"}},
				Cmds: []Cmd{{Cmd: "true"}},
			},
			"prereq": {
				Cmds: []Cmd{{Cmd: "echo prereq"}},
			},
		},
	}
	r := newTestRunner(t, tf, dir)
	execs := captureExecs(r)

	require.NoError(t, r.Run("parent", ""))

	// Find the prereq exec and check its env doesn't carry PARENT_ONLY.
	var prereqExec *Execution
	for i := range *execs {
		if (*execs)[i].Task == "prereq" {
			prereqExec = &(*execs)[i]
		}
	}
	require.NotNil(t, prereqExec, "prereq should have run")
	assert.Empty(t, envValue(prereqExec.Env, "PARENT_ONLY"))
}

func TestSubTaskCallBypassesMemoizationWhenParentEnvDiffers(t *testing.T) {
	// Two parents that both call the same child must each get their own
	// child execution with their own env. Without bypass, the second call
	// would short-circuit on r.runs and silently drop the second env.
	dir := t.TempDir()

	var childCalls atomic.Int64
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"all": {
				Cmds: []Cmd{
					{Task: "p1"},
					{Task: "p2"},
				},
			},
			"p1": {
				Env:  map[string]string{"WHO": "one"},
				Cmds: []Cmd{{Task: "child"}},
			},
			"p2": {
				Env:  map[string]string{"WHO": "two"},
				Cmds: []Cmd{{Task: "child"}},
			},
			"child": {
				Cmds: []Cmd{{Cmd: "echo ${WHO}"}},
			},
		},
	}
	r := newTestRunner(t, tf, dir)
	execs := captureExecs(r)
	r.ShellRunner = &fakeShellRunner{
		execs: execs,
		runFunc: func(req ShellCommand) error {
			if req.Kind == ShellCommandTask && req.TaskName == "child" {
				childCalls.Add(1)
			}
			return nil
		},
	}

	require.NoError(t, r.Run("all", ""))

	assert.Equal(t, int64(2), childCalls.Load(), "child must run once per call site")

	whos := []string{}
	for _, e := range *execs {
		if e.Task == "child" {
			whos = append(whos, envValue(e.Env, "WHO"))
		}
	}
	slices.Sort(whos)
	assert.Equal(t, []string{"one", "two"}, whos)
}

func TestRunPublicAPIDoesNotPropagateEnv(t *testing.T) {
	// Runner.Run is the public top-level entry point: there is no parent
	// task, so no env propagation. This guards the contract that tests and
	// programmatic callers don't accidentally pull the calling shell's env
	// into a task that was previously isolated.
	dir := t.TempDir()
	t.Setenv("LEAK_FROM_CALLER", "should-not-appear")
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"only-mine": {
				Env:  map[string]string{"OWN": "yes"},
				Cmds: []Cmd{{Cmd: "true"}},
			},
		},
	}
	r := newTestRunner(t, tf, dir) // BaseEnv = nil, so OS env is not in BaseEnv
	execs := captureExecs(r)

	require.NoError(t, r.Run("only-mine", ""))

	require.Len(t, *execs, 1)
	assert.Equal(t, "yes", envValue((*execs)[0].Env, "OWN"))
	assert.Empty(t, envValue((*execs)[0].Env, "LEAK_FROM_CALLER"))
}

func TestParentVarsAlsoFlowDownAsEnv(t *testing.T) {
	// Vars get exported as env entries by buildEnv (existing behaviour);
	// since parent's env propagates, parent's vars-as-env do too. Verifying
	// here so a future change can't silently drop this. Vars are NOT
	// substituted into the child's cmd template (that's a separate code
	// path), but they DO show up in the env passed to the shell.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"parent": {
				Vars: map[string]Var{"PARENT_VAR": {Value: "hello"}},
				Cmds: []Cmd{{Task: "child"}},
			},
			"child": {
				Cmds: []Cmd{{Cmd: "true"}},
			},
		},
	}
	r := newTestRunner(t, tf, dir)
	execs := captureExecs(r)

	require.NoError(t, r.Run("parent", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, "hello", envValue((*execs)[0].Env, "PARENT_VAR"))
}
