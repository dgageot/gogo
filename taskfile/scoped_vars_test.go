package taskfile

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNamespacedVarsIsolateSiblings is the regression test for the
// "running proxy:dev resolves assistant's VERSION" bug: each include declares
// its own vars and a `sh:` var that only resolves correctly in the include's
// own directory. Before scoping, running a task in one sibling would
// force-resolve every other sibling's `sh:` vars against the root dir and
// fail (or worse, succeed with a wrong value).
func TestNamespacedVarsIsolateSiblings(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
includes:
  - proxy
  - assistant
`,
		"proxy/gogo.yaml": `version: "1"
vars:
  LDFLAGS: -X main.Pkg=proxy
tasks:
  build:
    cmd: go build -ldflags '{{.LDFLAGS}}'
`,
		"assistant/gogo.yaml": `version: "1"
vars:
  LDFLAGS: -X main.Pkg=assistant
  VERSION:
    sh: cat VERSION
tasks:
  build:
    cmd: go build -ldflags '{{.LDFLAGS}}' --version {{.VERSION}}
`,
		"assistant/VERSION": "v8",
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	// Each include keeps its own LDFLAGS — sibling siblings stay invisible.
	assert.NotContains(t, tf.Vars, "LDFLAGS", "include vars must not leak into the root")
	assert.Equal(t, "-X main.Pkg=proxy", tf.NamespaceVars["proxy"]["LDFLAGS"].Value)
	assert.Equal(t, "-X main.Pkg=assistant", tf.NamespaceVars["assistant"]["LDFLAGS"].Value)

	// Running proxy:build must NOT trigger the assistant's `sh: cat VERSION`.
	runner := newTestRunner(t, tf, dir)
	shell := &fakeShellRunner{}
	runner.ShellRunner = shell
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("proxy:build", ""))

	require.Len(t, *execs, 1)
	assert.Equal(t, "go build -ldflags '-X main.Pkg=proxy'", (*execs)[0].Command)
	// Critically, the assistant's `cat VERSION` was never invoked.
	for _, out := range shell.outputsSnapshot() {
		assert.NotContains(t, out.Command, "cat VERSION",
			"resolving proxy:build must not run sibling assistant's `sh:` vars")
	}
}

// TestNamespacedShVarRunsInIncludeDir locks in that an included var's `sh:`
// command is executed with the include's own working directory, not the
// root's. Previously every var's `sh:` ran from r.tf.Dir, which broke
// patterns like `grep "^#agent_version:" gordon.yaml` written from inside
// the include's directory.
func TestNamespacedShVarRunsInIncludeDir(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
includes:
  - assistant
`,
		"assistant/gogo.yaml": `version: "1"
vars:
  VERSION:
    sh: cat VERSION
tasks:
  show:
    cmd: echo {{.VERSION}}
`,
		"assistant/VERSION": "v8",
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	runner := newTestRunner(t, tf, dir)
	shell := &fakeShellRunner{
		outputFunc: func(req ShellCommand) ([]byte, error) {
			// Faithful echo: assert the cmd ran in the include's own dir.
			assert.Equal(t, filepath.Join(dir, "assistant"), req.Dir,
				"`sh:` var declared in an include must execute in that include's directory")
			return []byte("v8\n"), nil
		},
	}
	runner.ShellRunner = shell
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("assistant:show", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, "echo v8", (*execs)[0].Command)
}

// TestShVarPreservesAwkPositional locks in the fix for a latent bug that
// the scoping change first surfaced: when expanding a `sh:` command, an
// awk positional reference like `$2` (or any other shell-special single-
// character name os.Expand recognises) must survive verbatim. Previously
// the unknown-var fallback always wrote `${KEY}`, which mangled awk's
// `{print $2}` into `{print ${2}}` and crashed it with a syntax error.
func TestShVarPreservesAwkPositional(t *testing.T) {
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Vars: map[string]Var{
			"VERSION": {Sh: `grep "^#agent_version:" file.yaml | awk '{print $2}'`},
		},
		Tasks: map[string]Task{
			"show": {Cmds: []Cmd{{Cmd: "echo {{.VERSION}}"}}},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	shell := &fakeShellRunner{
		outputFunc: func(req ShellCommand) ([]byte, error) {
			assert.Equal(t, `grep "^#agent_version:" file.yaml | awk '{print $2}'`, req.Command,
				"awk positionals must reach the shell unchanged")
			return []byte("v8\n"), nil
		},
	}
	runner.ShellRunner = shell
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("show", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, "echo v8", (*execs)[0].Command)
}

// TestExpandVarsPreservesAwkPositional covers the same preservation rule at
// the unit level for command strings (the path the shell ultimately runs).
func TestExpandVarsPreservesAwkPositional(t *testing.T) {
	// Single-character shell-special names ($1–$9, $@, $#, $*, etc.) are
	// kept as-is so awk/sed/regex syntax survives the template pass.
	assert.Equal(t,
		`awk '{print $2}'`,
		expandVars(`awk '{print $2}'`, nil, "", nil),
	)
	assert.Equal(t,
		`echo $@`,
		expandVars(`echo $@`, nil, "", nil),
	)

	// Shell references are left exactly as written for the shell to expand.
	assert.Equal(t,
		`echo ${UNKNOWN}`,
		expandVars(`echo ${UNKNOWN}`, nil, "", nil),
	)
	assert.Equal(t,
		`echo $UNKNOWN`,
		expandVars(`echo $UNKNOWN`, nil, "", nil),
	)
}

// TestNamespacedVarsVisibleFromNestedTask covers the contract that an
// include's vars are visible to its own tasks (the user-facing payoff —
// `{{.LDFLAGS}}` written inside the include just works) and to deeper
// nested includes (so a "platform" include can publish shared vars to
// every leaf module under it).
func TestNamespacedVarsVisibleFromNestedTask(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
includes:
  - platform
`,
		"platform/gogo.yaml": `version: "1"
includes:
  - service
vars:
  REGISTRY: docker.io
tasks:
  whoami:
    cmd: echo {{.REGISTRY}}
`,
		"platform/service/gogo.yaml": `version: "1"
tasks:
  publish:
    cmd: docker push {{.REGISTRY}}/app
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("platform:whoami", ""))
	require.NoError(t, runner.Run("platform:service:publish", ""))

	require.Len(t, *execs, 2)
	assert.Equal(t, "echo docker.io", (*execs)[0].Command)
	assert.Equal(t, "docker push docker.io/app", (*execs)[1].Command,
		"nested include must inherit ancestor namespace vars")
}

// TestRootVarsVisibleFromIncludeTask ensures that vars declared at the
// root are visible everywhere — the include can rely on globally shared
// values without re-declaring them.
func TestRootVarsVisibleFromIncludeTask(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
includes:
  - cli
vars:
  REGISTRY: docker.io
`,
		"cli/gogo.yaml": `version: "1"
tasks:
  push:
    cmd: docker push {{.REGISTRY}}/cli
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("cli:push", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, "docker push docker.io/cli", (*execs)[0].Command)
}

// TestNamespacedVarsBeatRootOnCollision codifies the precedence: the
// most-specific namespace wins over the root. This is what lets each
// include redefine a shared name (e.g. LDFLAGS) for itself.
func TestNamespacedVarsBeatRootOnCollision(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
includes:
  - cli
vars:
  LDFLAGS: -X root=true
`,
		"cli/gogo.yaml": `version: "1"
vars:
  LDFLAGS: -X cli=true
tasks:
  build:
    cmd: go build -ldflags '{{.LDFLAGS}}'
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("cli:build", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, "go build -ldflags '-X cli=true'", (*execs)[0].Command,
		"include's LDFLAGS must win over the root's LDFLAGS")
}

// TestFlattenInsideIncludeScopesVarsToNamespace verifies that flatten files
// pulled in from an include contribute their vars to the include's
// namespace (not the root) — matching how their tasks are merged.
func TestFlattenInsideIncludeScopesVarsToNamespace(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
includes:
  - cli
`,
		"cli/gogo.yaml": `version: "1"
flatten:
  - extras.yml
`,
		"cli/extras.yml": `version: "1"
vars:
  HELPER: from-flatten
tasks:
  show:
    cmd: echo {{.HELPER}}
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	// HELPER landed at the cli namespace, not at the root.
	assert.NotContains(t, tf.Vars, "HELPER")
	assert.Equal(t, "from-flatten", tf.NamespaceVars["cli"]["HELPER"].Value)

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("cli:show", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, "echo from-flatten", (*execs)[0].Command)
}
