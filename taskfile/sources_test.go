package taskfile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveSourcesBuiltinGo(t *testing.T) {
	got, err := resolveSources(builtinSourcePresets(), []string{"go"})
	require.NoError(t, err)
	assert.Equal(t, []string{"**/*.go", "go.mod", "go.sum"}, got)
}

func TestResolveSourcesBuiltinGoVendoredComposesGo(t *testing.T) {
	got, err := resolveSources(builtinSourcePresets(), []string{"go-vendored"})
	require.NoError(t, err)
	assert.Equal(t, []string{"**/*.go", "go.mod", "go.sum", "vendor/**"}, got)
}

func TestResolveSourcesUnknownNameTreatedAsLiteral(t *testing.T) {
	// "go.mod" has no glob characters and isn't a preset name — keep as-is so
	// existing files like "go.mod" or ".golangci.yml" still work without
	// users having to invent presets for them.
	got, err := resolveSources(builtinSourcePresets(), []string{"go.mod", ".golangci.yml"})
	require.NoError(t, err)
	assert.Equal(t, []string{"go.mod", ".golangci.yml"}, got)
}

func TestResolveSourcesGlobsArePreservedVerbatim(t *testing.T) {
	got, err := resolveSources(builtinSourcePresets(), []string{"**/*.proto", "cmd/*.go"})
	require.NoError(t, err)
	assert.Equal(t, []string{"**/*.proto", "cmd/*.go"}, got)
}

func TestResolveSourcesMixedPresetAndLiterals(t *testing.T) {
	presets := builtinSourcePresets()
	presets["lint"] = StringList{"go", ".golangci.yml"}

	got, err := resolveSources(presets, []string{"lint"})
	require.NoError(t, err)
	assert.Equal(t, []string{"**/*.go", "go.mod", "go.sum", ".golangci.yml"}, got)
}

func TestResolveSourcesUserOverridesBuiltin(t *testing.T) {
	user := map[string]StringList{"go": {"**/*.go"}} // strip go.mod/go.sum
	got, err := resolveSources(effectivePresets(user), []string{"go"})
	require.NoError(t, err)
	assert.Equal(t, []string{"**/*.go"}, got)
}

func TestResolveSourcesDeduplicates(t *testing.T) {
	presets := map[string]StringList{
		"a": {"**/*.go", "go.mod"},
		"b": {"go.mod", "go.sum"},
	}
	got, err := resolveSources(effectivePresets(presets), []string{"a", "b", "**/*.go"})
	require.NoError(t, err)
	assert.Equal(t, []string{"**/*.go", "go.mod", "go.sum"}, got)
}

func TestResolveSourcesCycleIsRejected(t *testing.T) {
	presets := map[string]StringList{
		"a": {"b"},
		"b": {"a"},
	}
	_, err := resolveSources(presets, []string{"a"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cyclic source preset")
	assert.Contains(t, err.Error(), "a -> b -> a")
}

func TestResolveSourcesEmptyEntriesSkipped(t *testing.T) {
	got, err := resolveSources(builtinSourcePresets(), []string{"", "go"})
	require.NoError(t, err)
	assert.Equal(t, []string{"**/*.go", "go.mod", "go.sum"}, got)
}

func TestParseTopLevelSourcesField(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gogo.yaml"), []byte(`version: "1"
sources:
  lint:
    - go
    - .golangci.yml
tasks:
  lint:
    cmd: golangci-lint run
    sources:
      - lint
`), 0o644))

	tf, err := Parse(dir)
	require.NoError(t, err)

	assert.Equal(t, StringList{"go", ".golangci.yml"}, tf.Sources["lint"])
	assert.Equal(t, StringList{"lint"}, tf.Tasks["lint"].Sources)

	resolved, err := tf.taskSources([]string(tf.Tasks["lint"].Sources))
	require.NoError(t, err)
	assert.Equal(t, []string{"**/*.go", "go.mod", "go.sum", ".golangci.yml"}, resolved)
}

func TestParseTaskSourcesAcceptsBareString(t *testing.T) {
	// `sources: go` is the short form; StringList already supports it via its
	// custom UnmarshalYAML, but make sure the preset resolver follows it.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gogo.yaml"), []byte(`version: "1"
tasks:
  build:
    cmd: go build .
    sources: go
`), 0o644))

	tf, err := Parse(dir)
	require.NoError(t, err)

	resolved, err := tf.taskSources([]string(tf.Tasks["build"].Sources))
	require.NoError(t, err)
	assert.Equal(t, []string{"**/*.go", "go.mod", "go.sum"}, resolved)
}

func TestSourcesChecksumWithGoPresetDetectsChange(t *testing.T) {
	// End-to-end sanity check: a task using `sources: go` must invalidate
	// when a Go file changes, even though the preset is referenced by name.
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"main.go":    "package main",
		"go.mod":     "module test",
		"go.sum":     "",
		"sub/lib.go": "package lib",
	})

	cfg := &Config{} // no user presets — built-ins only
	resolved, err := cfg.taskSources([]string{"go"})
	require.NoError(t, err)

	sum1, err := sourcesChecksum(dir, resolved)
	require.NoError(t, err)
	require.NotEmpty(t, sum1)

	writeFiles(t, dir, map[string]string{"sub/lib.go": "package lib2"})

	sum2, err := sourcesChecksum(dir, resolved)
	require.NoError(t, err)
	assert.NotEqual(t, sum1, sum2)
}

func TestLoadWithIncludesMergesSourcePresetsRootWins(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
includes:
  - cli
sources:
  shared: ["**/*.go", "go.mod"]
`,
		"cli/gogo.yaml": `version: "1"
sources:
  shared: ["should-lose"]
  cli-only: ["**/cli/*.go"]
tasks:
  build:
    cmd: go build .
    sources: [shared, cli-only]
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	assert.Equal(t, StringList{"**/*.go", "go.mod"}, tf.Sources["shared"], "root preset wins")
	assert.Equal(t, StringList{"**/cli/*.go"}, tf.Sources["cli-only"], "include preset is merged in")

	resolved, err := tf.taskSources([]string(tf.Tasks["cli:build"].Sources))
	require.NoError(t, err)
	assert.Equal(t, []string{"**/*.go", "go.mod", "**/cli/*.go"}, resolved)
}

func TestRunnerSkipsTaskWithUnchangedPresetSources(t *testing.T) {
	// Integration test: a task using `sources: go` becomes "up to date" after
	// a successful run, the same way a literal source list would.
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"main.go": "package main",
		"go.mod":  "module test",
		"go.sum":  "",
	})

	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Cmds:    []Cmd{{Cmd: "true"}},
				Sources: StringList{"go"},
			},
		},
	}
	r := newTestRunner(t, tf, dir)

	require.NoError(t, r.Run("build", ""))
	r.ResetRan()

	// Second invocation must short-circuit on the stored checksum.
	execs := captureExecs(r)
	*execs = nil
	require.NoError(t, r.Run("build", ""))
	assert.Empty(t, *execs, "second run should be skipped as up-to-date")
}
