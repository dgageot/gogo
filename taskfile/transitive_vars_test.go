package taskfile

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runCmdAndCapture runs `task` once and returns the resolved cmd string the
// shell runner would have received. It's a thin convenience around the
// fakeShellRunner / captureExecs pair used elsewhere.
func runCmdAndCapture(t *testing.T, tf *Config) string {
	t.Helper()
	r := newTestRunner(t, tf, tf.Dir)
	execs := captureExecs(r)
	require.NoError(t, r.Run("show", ""))
	require.Len(t, *execs, 1)
	return (*execs)[0].Command
}

func TestVarsTransitiveExpansionSimpleChain(t *testing.T) {
	// The headline bug: `{{.A}}` referenced from another var's value used to
	// land in the final cmd as a literal. Now it resolves transitively.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Vars: map[string]Var{
			"GREETING": {Value: "hello"},
			"MESSAGE":  {Value: "{{.GREETING}} world"},
		},
		Tasks: map[string]Task{
			"show": {Cmds: []Cmd{{Cmd: "echo {{.MESSAGE}}"}}},
		},
	}
	assert.Equal(t, "echo hello world", runCmdAndCapture(t, tf))
}

func TestVarsTransitiveExpansionForwardReference(t *testing.T) {
	// Map iteration is randomised, so this also catches any "must declare
	// before use" assumption \u2014 A references B even though B is alphabetically
	// later. Lazy resolution makes declaration order irrelevant.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Vars: map[string]Var{
			"A": {Value: "{{.B}}"},
			"B": {Value: "resolved"},
		},
		Tasks: map[string]Task{
			"show": {Cmds: []Cmd{{Cmd: "echo {{.A}}"}}},
		},
	}
	assert.Equal(t, "echo resolved", runCmdAndCapture(t, tf))
}

func TestVarsTransitiveExpansionShellSyntax(t *testing.T) {
	// ${VAR} works anywhere {{.VAR}} works. Same single source of truth.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Vars: map[string]Var{
			"A": {Value: "alpha"},
			"B": {Value: "${A}-beta"},
		},
		Tasks: map[string]Task{
			"show": {Cmds: []Cmd{{Cmd: "echo {{.B}}"}}},
		},
	}
	assert.Equal(t, "echo alpha-beta", runCmdAndCapture(t, tf))
}

func TestVarsTransitiveCycleResolvesToEmpty(t *testing.T) {
	// Cycles must not loop; matching how task.env cross-references behave,
	// the cycle short-circuits to an empty value so the rest of the
	// expansion can complete.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Vars: map[string]Var{
			"A": {Value: "[{{.B}}]"},
			"B": {Value: "[{{.A}}]"},
		},
		Tasks: map[string]Task{
			"show": {Cmds: []Cmd{{Cmd: "echo {{.A}}"}}},
		},
	}
	// A -> B -> A (cycle, "" returned) -> B becomes "[]" -> A becomes "[[]]"
	assert.Equal(t, "echo [[]]", runCmdAndCapture(t, tf))
}

func TestVarsTransitiveSelfCycleResolvesToEmpty(t *testing.T) {
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Vars: map[string]Var{
			"X": {Value: "[{{.X}}]"},
		},
		Tasks: map[string]Task{
			"show": {Cmds: []Cmd{{Cmd: "echo {{.X}}"}}},
		},
	}
	assert.Equal(t, "echo []", runCmdAndCapture(t, tf))
}

func TestVarsTransitiveExpansionLDFLAGSPattern(t *testing.T) {
	// The exact cagent-proxy / gordon shape that motivated this fix. Before
	// the change, {{.LDFLAGS}} substituted into the cmd carried `{{.GIT_TAG}}`
	// and `{{.GIT_COMMIT}}` along as literals.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Vars: map[string]Var{
			"GIT_TAG":    {Value: "v1.2.3"},
			"GIT_COMMIT": {Value: "abcdef0"},
			"LDFLAGS":    {Value: `-X main.Version={{.GIT_TAG}} -X main.Commit={{.GIT_COMMIT}}`},
		},
		Tasks: map[string]Task{
			"show": {Cmds: []Cmd{{Cmd: `go build -ldflags '{{.LDFLAGS}}' .`}}},
		},
	}
	assert.Equal(t,
		`go build -ldflags '-X main.Version=v1.2.3 -X main.Commit=abcdef0' .`,
		runCmdAndCapture(t, tf),
	)
}

func TestVarsReferenceBuiltinGitVars(t *testing.T) {
	// After PR #5, GIT_COMMIT is a built-in. A user-defined LDFLAGS that
	// references it without re-declaring the var must still resolve.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Vars: map[string]Var{
			"LDFLAGS": {Value: "-X main.Commit={{.GIT_COMMIT}}"},
		},
		Tasks: map[string]Task{
			"show": {Cmds: []Cmd{{Cmd: "echo {{.LDFLAGS}}"}}},
		},
	}
	r := newTestRunner(t, tf, dir)
	r.ShellRunner = &fakeShellRunner{
		outputFunc: func(req ShellCommand) ([]byte, error) {
			if req.Command == "git rev-parse HEAD" {
				return []byte("deadbeef\n"), nil
			}
			return nil, errors.New("unexpected: " + req.Command)
		},
	}
	execs := captureExecs(r)

	require.NoError(t, r.Run("show", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, "echo -X main.Commit=deadbeef", (*execs)[0].Command)
}

func TestVarsUserOverrideStillBeatsBuiltin(t *testing.T) {
	// A user-defined GIT_COMMIT must override the built-in even when consumed
	// transitively via another var. Guards the precedence rule.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Vars: map[string]Var{
			"GIT_COMMIT": {Value: "user-pinned"},
			"LDFLAGS":    {Value: "-X main.Commit={{.GIT_COMMIT}}"},
		},
		Tasks: map[string]Task{
			"show": {Cmds: []Cmd{{Cmd: "echo {{.LDFLAGS}}"}}},
		},
	}
	assert.Equal(t, "echo -X main.Commit=user-pinned", runCmdAndCapture(t, tf))
}

func TestVarsShCommandSeesOtherVars(t *testing.T) {
	// The `sh:` form is template-expanded too, so a shell command can be
	// parameterised by other vars. This unblocks patterns like
	// `sh: ls -1 dist/{{.IMAGE}}-*.tar`.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Vars: map[string]Var{
			"NAME":  {Value: "world"},
			"GREET": {Sh: "echo hello-{{.NAME}}"},
		},
		Tasks: map[string]Task{
			"show": {Cmds: []Cmd{{Cmd: "echo {{.GREET}}"}}},
		},
	}
	r := newTestRunner(t, tf, dir)
	// fakeShellRunner.Output returns "sh:" + Command by default \u2014 use it as
	// a faithful echo so we can verify the templated cmdline reached the
	// shell after expansion.
	execs := captureExecs(r)

	require.NoError(t, r.Run("show", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, "echo sh:echo hello-world", (*execs)[0].Command)
}

func TestVarsTaskScopeOverridesGlobal(t *testing.T) {
	// Pre-existing precedence (task vars > global vars) must still hold
	// after the lazy refactor.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Vars: map[string]Var{
			"ENV": {Value: "global"},
		},
		Tasks: map[string]Task{
			"show": {
				Vars: map[string]Var{"ENV": {Value: "task"}},
				Cmds: []Cmd{{Cmd: "echo {{.ENV}}"}},
			},
		},
	}
	assert.Equal(t, "echo task", runCmdAndCapture(t, tf))
}

func TestVarsExtraVarsOverrideTaskVarsAndExpandTransitively(t *testing.T) {
	// Call-site `vars:` should override the called task's own vars *and*
	// participate in transitive expansion \u2014 a value passed in at the call
	// site can reference the task's other vars by name.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"caller": {
				Cmds: []Cmd{{
					Task: "callee",
					Vars: map[string]Var{
						"GREETING": {Value: "Hi from {{.LOCATION}}"},
					},
				}},
			},
			"callee": {
				Vars: map[string]Var{
					"GREETING": {Value: "default"},
					"LOCATION": {Value: "Paris"},
				},
				Cmds: []Cmd{{Cmd: "echo {{.GREETING}}"}},
			},
		},
	}
	r := newTestRunner(t, tf, dir)
	execs := captureExecs(r)

	require.NoError(t, r.Run("caller", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, "echo Hi from Paris", (*execs)[0].Command)
}

func TestVarsShCommandFailureIsSurfaced(t *testing.T) {
	// A failing `sh:` should still propagate as an error to the caller, so
	// users learn about the problem instead of silently getting "".
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Vars: map[string]Var{
			"BAD": {Sh: "exit 1"},
		},
		Tasks: map[string]Task{
			"show": {Cmds: []Cmd{{Cmd: "echo {{.BAD}}"}}},
		},
	}
	r := newTestRunner(t, tf, dir)
	r.ShellRunner = &fakeShellRunner{
		outputFunc: func(req ShellCommand) ([]byte, error) {
			return nil, errors.New("exit 1")
		},
	}

	err := r.Run("show", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolving variable (sh: exit 1)")
}

func TestVarsTaskFileDirIsAlwaysAvailable(t *testing.T) {
	// The pre-existing TASK_FILE_DIR built-in must still be resolvable from
	// inside another var's body, not just inside cmds.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Vars: map[string]Var{
			"OUT_PATH": {Value: "{{.TASK_FILE_DIR}}/dist"},
		},
		Tasks: map[string]Task{
			"show": {Cmds: []Cmd{{Cmd: "echo {{.OUT_PATH}}"}}},
		},
	}
	assert.Equal(t, "echo "+dir+"/dist", runCmdAndCapture(t, tf))
}
