package taskfile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- validation -------------------------------------------------------------

func TestLoadWithIncludesRejectsTaskReferencingUnknownSecret(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gogo.yaml"), []byte(`version: "1"
secrets:
  KNOWN: op://vault/item/field
tasks:
  test:
    secrets: [TYPO]
    cmd: true
`), 0o644))

	_, err := LoadWithIncludes(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"TYPO"`)
	assert.Contains(t, err.Error(), `"test"`)
}

func TestLoadWithIncludesRejectsUnknownBackend(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gogo.yaml"), []byte(`version: "1"
secrets:
  X: vault://somewhere/else
tasks:
  test:
    cmd: true
`), 0o644))

	_, err := LoadWithIncludes(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown backend")
	assert.Contains(t, err.Error(), "op://")
}

func TestLoadWithIncludesAcceptsValidSecretsBlock(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gogo.yaml"), []byte(`version: "1"
secrets:
  OPENAI_API_KEY: op://Team/docker-ai/OPENAI_API_KEY
  ANTHROPIC_KEY:  op://Team/docker-ai/ANTHROPIC_API_KEY
tasks:
  test:
    secrets: [OPENAI_API_KEY, ANTHROPIC_KEY]
    cmd: true
`), 0o644))

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)
	assert.Equal(t, "op://Team/docker-ai/OPENAI_API_KEY", tf.Secrets["OPENAI_API_KEY"])
	assert.Equal(t, "op://Team/docker-ai/ANTHROPIC_API_KEY", tf.Secrets["ANTHROPIC_KEY"])
	assert.Equal(t, StringList{"OPENAI_API_KEY", "ANTHROPIC_KEY"}, tf.Tasks["test"].Secrets)
}

func TestLoadWithIncludesMergesSecretsRootWins(t *testing.T) {
	// Mirrors mergeVars / mergeSourcePresets precedence: child includes can
	// declare secrets but the root file's declarations win on conflicts.
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
includes: [cli]
secrets:
  SHARED: op://root/item/field
`,
		"cli/gogo.yaml": `version: "1"
secrets:
  SHARED:   op://child/item/field
  CLI_ONLY: op://cli/item/field
tasks:
  build:
    secrets: [SHARED, CLI_ONLY]
    cmd: true
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)
	assert.Equal(t, "op://root/item/field", tf.Secrets["SHARED"], "root preset wins")
	assert.Equal(t, "op://cli/item/field", tf.Secrets["CLI_ONLY"], "child entry merged in")
}

func TestLoadWithIncludesRejectsOpSyntaxInVars(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gogo.yaml"), []byte(`version: "1"
vars:
  TOKEN: op://vault/item/field
tasks:
  test:
    cmd: true
`), 0o644))

	_, err := LoadWithIncludes(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `variable "TOKEN"`)
	assert.Contains(t, err.Error(), "only supported in env")
}

func TestLoadWithIncludesRejectsOpSyntaxInNamespacedVars(t *testing.T) {
	// A var declared in an included gogo.yaml lands in NamespaceVars, not
	// the root Vars map. It must still be rejected: vars never flow through
	// op run, so an op:// URI there would leak verbatim into commands.
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
includes: [cli]
`,
		"cli/gogo.yaml": `version: "1"
vars:
  TOKEN: op://vault/item/field
tasks:
  build:
    cmd: true
`,
	})

	_, err := LoadWithIncludes(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `namespace "cli" vars`)
	assert.Contains(t, err.Error(), `variable "TOKEN"`)
	assert.Contains(t, err.Error(), "only supported in env")
}

func TestSecretsOpPassThroughTriggersOpRun(t *testing.T) {
	// op:// is pass-through: the URI itself becomes the env value and
	// hasOpSecrets triggers the op-run wrapper at exec time.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Secrets: map[string]string{
			"OPENAI_API_KEY": "op://Team/docker-ai/OPENAI_API_KEY",
		},
		Tasks: map[string]Task{
			"test": {
				Secrets: StringList{"OPENAI_API_KEY"},
				Cmds:    []Cmd{{Cmd: "true"}},
			},
		},
	}
	r := newTestRunner(t, tf, dir)
	execs := captureExecs(r)

	require.NoError(t, r.Run("test", ""))

	require.Len(t, *execs, 1)
	assert.Equal(t, "op://Team/docker-ai/OPENAI_API_KEY",
		envValue((*execs)[0].Env, "OPENAI_API_KEY"))
	assert.True(t, (*execs)[0].UseOpRun, "op:// in resolved env must wrap exec in op run")
}

func TestSecretsBeatTaskEnvWithSameKey(t *testing.T) {
	// If a task declares both `env: { K: dummy }` and `secrets: [K]`, the
	// secrets resolution layer wins. Declaring a secret is a strong signal
	// that K should come from the secrets backend, not a placeholder.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Secrets: map[string]string{
			"OPENAI_API_KEY": "op://team/item/field",
		},
		Tasks: map[string]Task{
			"test": {
				Env:     map[string]string{"OPENAI_API_KEY": "DUMMY"},
				Secrets: StringList{"OPENAI_API_KEY"},
				Cmds:    []Cmd{{Cmd: "true"}},
			},
		},
	}
	r := newTestRunner(t, tf, dir)
	execs := captureExecs(r)

	require.NoError(t, r.Run("test", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, "op://team/item/field", envValue((*execs)[0].Env, "OPENAI_API_KEY"))
}

func TestSecretsParentEnvPropagatesResolvedSecretsToSubTasks(t *testing.T) {
	// The parent->child env propagation behaviour should carry resolved
	// secrets down to sub-tasks just like any other env entry.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Secrets: map[string]string{
			"OPENAI_API_KEY": "op://team/item/field",
		},
		Tasks: map[string]Task{
			"parent": {
				Secrets: StringList{"OPENAI_API_KEY"},
				Cmds:    []Cmd{{Task: "child"}},
			},
			"child": {
				Cmds: []Cmd{{Cmd: "true"}},
			},
		},
	}
	r := newTestRunner(t, tf, dir)
	execs := captureExecs(r)

	require.NoError(t, r.Run("parent", ""))

	var child *Execution
	for i := range *execs {
		if (*execs)[i].Task == "child" {
			child = &(*execs)[i]
		}
	}
	require.NotNil(t, child)
	assert.Equal(t, "op://team/item/field", envValue(child.Env, "OPENAI_API_KEY"))
	assert.True(t, child.UseOpRun, "op:// flowed down to the child must keep wrapping in op run")
}

func TestSecretsTaskWithoutSecretsBlockUnaffected(t *testing.T) {
	// Tasks that don't declare `secrets:` see no change in behaviour, even
	// when a top-level secrets: block is defined. Per-task allow-list.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Secrets: map[string]string{
			"OPENAI_API_KEY": "op://team/item/field",
		},
		Tasks: map[string]Task{
			"clean": {Cmds: []Cmd{{Cmd: "rm -rf bin"}}},
		},
	}
	r := newTestRunner(t, tf, dir)
	execs := captureExecs(r)

	require.NoError(t, r.Run("clean", ""))
	require.Len(t, *execs, 1)
	assert.Empty(t, envValue((*execs)[0].Env, "OPENAI_API_KEY"))
	assert.False(t, (*execs)[0].UseOpRun)
}

func TestSecretsExistingOpInEnvStillTriggersOpRun(t *testing.T) {
	// Backward compatibility: users who already have `env: { X: op://... }`
	// without a top-level secrets: block must continue to work exactly as
	// before. The hasOpSecrets path runs after the secrets layer, so any
	// op:// in env (from any source) still wraps in op run.
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"test": {
				Env:  map[string]string{"OPENAI_API_KEY": "op://Team/docker-ai/OPENAI_API_KEY"},
				Cmds: []Cmd{{Cmd: "true"}},
			},
		},
	}
	r := newTestRunner(t, tf, dir)
	execs := captureExecs(r)

	require.NoError(t, r.Run("test", ""))

	require.Len(t, *execs, 1)
	assert.True(t, (*execs)[0].UseOpRun)
}
