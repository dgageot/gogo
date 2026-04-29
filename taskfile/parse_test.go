package taskfile

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
includes:
  - cli
tasks:
  todo:
    cmd: open TODO.md
`,
		"cli/gogo.yaml": `version: "1"
tasks:
  # Build the docker-ai CLI
  build:
    cmd: go build .
  cli_internal:
    cmd: echo hidden
  # Install the docker-ai CLI
  install:
    deps:
      - build
    cmd: go install .
  # GitHub helper
  github:
    aliases:
      - gh
    cmd: gh auth status
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	assert.Equal(t, "1", tf.Version)
	assert.NotEmpty(t, tf.Tasks)

	task, ok := tf.Tasks["todo"]
	require.True(t, ok)
	assert.Empty(t, task.Desc)

	task, ok = tf.Tasks["cli:build"]
	require.True(t, ok)
	assert.Equal(t, "Build the docker-ai CLI", task.Desc)

	task, ok = tf.Tasks["cli:install"]
	require.True(t, ok)
	assert.Len(t, task.Deps, 1)
	assert.Equal(t, "cli:build", task.Deps[0].Task)
	assert.Equal(t, "Install the docker-ai CLI", task.Desc)

	task, ok = tf.Tasks["cli:github"]
	require.True(t, ok)
	assert.Equal(t, StringList{"gh"}, task.Aliases)
	assert.Equal(t, "GitHub helper", task.Desc)
}

func TestLoadWithIncludesNested(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
includes:
  - cli
`,
		"cli/gogo.yaml": `version: "1"
includes:
  - nested
tasks:
  build:
    cmd: go build .
`,
		"cli/nested/gogo.yaml": `version: "1"
tasks:
  test:
    cmd: go test ./...
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	assert.Contains(t, tf.Tasks, "cli:build")
	assert.Contains(t, tf.Tasks, "cli:nested:test")
}

func TestLoadWithIncludesRejectsCycles(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
includes:
  - cli
`,
		"cli/gogo.yaml": `version: "1"
includes:
  - root
`,
		"cli/root/gogo.yaml": `version: "1"
includes:
  - ../..
`,
	})

	_, err := LoadWithIncludes(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cyclic include")
	assert.Contains(t, err.Error(), filepath.Join(dir, "cli", "root", "gogo.yaml"))
}

func TestLoadWithIncludesMissingMentionsParentFile(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
includes:
  - backend
`,
		"backend/gogo.yaml": `version: "1"
includes:
  - data
`,
	})

	_, err := LoadWithIncludes(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `loading include "backend:data"`)
	assert.Contains(t, err.Error(), "from "+filepath.Join(dir, "backend", "gogo.yaml"))
	assert.Contains(t, err.Error(), "no gogo.yaml found")
}

func TestLoadWithIncludesPreservesChildVars(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
includes:
  - child
tasks:
  hello:
    cmd: echo hello
`,
		"child/gogo.yaml": `version: "1"
vars:
  VERSION: "1.2.3"
tasks:
  build:
    cmd: echo {{.VERSION}}
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	assert.Contains(t, tf.Vars, "VERSION")
	assert.Equal(t, "1.2.3", tf.Vars["VERSION"].Value)
}

func TestLoadWithIncludesNestedNamespaceCollision(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
includes:
  - cli
  - server
`,
		"cli/gogo.yaml": `version: "1"
includes:
  - utils
tasks:
  build:
    cmd: go build ./cli
`,
		"cli/utils/gogo.yaml": `version: "1"
tasks:
  fmt:
    cmd: gofmt ./cli/utils
`,
		"server/gogo.yaml": `version: "1"
includes:
  - utils
tasks:
  build:
    cmd: go build ./server
`,
		"server/utils/gogo.yaml": `version: "1"
tasks:
  fmt:
    cmd: gofmt ./server/utils
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	assert.Contains(t, tf.Tasks, "cli:build")
	assert.Contains(t, tf.Tasks, "server:build")
	assert.Contains(t, tf.Tasks, "cli:utils:fmt", "nested include under cli should be namespaced as cli:utils:fmt")
	assert.Contains(t, tf.Tasks, "server:utils:fmt", "nested include under server should be namespaced as server:utils:fmt")
}

func TestExpandTemplates(t *testing.T) {
	t.Setenv("MY_VAR", "hello")

	assert.Equal(t, []byte("value: hello"), expandTemplates([]byte("value: {{.MY_VAR}}")))
	assert.Equal(t, []byte("value: hello"), expandTemplates([]byte("value: {{ .MY_VAR }}")))
	assert.Equal(t, []byte("value: {{.UNSET_VAR}}"), expandTemplates([]byte("value: {{.UNSET_VAR}}")))
}

func TestParsePreconditions(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gogo.yaml"), []byte(`version: "1"
tasks:
  deploy:
    preconditions:
      - sh: test -n "$DOCKER_HUB_USER"
        msg: DOCKER_HUB_USER is not set
      - test -n "$DOCKER_HUB_PASSWORD"
    cmd: echo deploying
`), 0o644))

	tf, err := Parse(dir)
	require.NoError(t, err)

	task := tf.Tasks["deploy"]
	require.Len(t, task.Preconditions, 2)
	assert.Equal(t, `test -n "$DOCKER_HUB_USER"`, task.Preconditions[0].Sh)
	assert.Equal(t, "DOCKER_HUB_USER is not set", task.Preconditions[0].Msg)
	assert.Equal(t, `test -n "$DOCKER_HUB_PASSWORD"`, task.Preconditions[1].Sh)
	assert.Empty(t, task.Preconditions[1].Msg)
}

func TestParseSilentField(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gogo.yaml"), []byte(`version: "1"
tasks:
  setup:
    silent: true
    cmds:
      - echo step1
      - echo step2
  build:
    cmds:
      - go build
`), 0o644))

	tf, err := Parse(dir)
	require.NoError(t, err)

	assert.True(t, tf.Tasks["setup"].Silent)
	assert.False(t, tf.Tasks["build"].Silent, "silent defaults to false")
}

func TestApplyTaskComments(t *testing.T) {
	yamlData := []byte(`version: "3"
tasks:
  # Build the project
  build:
    cmd: go build
  # Run all the tests
  test:
    cmd: go test
  deploy:
    cmd: deploy.sh
`)

	tf := &Config{
		Tasks: map[string]Task{
			"build":  {Cmds: []Cmd{{Cmd: "go build"}}},
			"test":   {Cmds: []Cmd{{Cmd: "go test"}}},
			"deploy": {Cmds: []Cmd{{Cmd: "deploy.sh"}}},
		},
	}

	applyTaskComments(tf, yamlData)

	assert.Equal(t, "Build the project", tf.Tasks["build"].Desc)
	assert.Equal(t, "Run all the tests", tf.Tasks["test"].Desc)
	assert.Empty(t, tf.Tasks["deploy"].Desc)
}

func TestLoadWithIncludesKeepsExternalTaskReferencesUnchanged(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
includes:
  - cli
tasks:
  shared:
    cmd: echo root
`,
		"cli/gogo.yaml": `version: "1"
tasks:
  build:
    deps:
      - shared
    cmds:
      - task: shared
      - task: test
  test:
    cmd: go test ./...
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	build := tf.Tasks["cli:build"]
	require.Len(t, build.Deps, 1)
	assert.Equal(t, "shared", build.Deps[0].Task)
	require.Len(t, build.Cmds, 2)
	assert.Equal(t, "shared", build.Cmds[0].Task)
	assert.Equal(t, "cli:test", build.Cmds[1].Task)
}

func TestLoadWithIncludesMakesTaskDirsAbsolute(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
includes:
  - cli
`,
		"cli/gogo.yaml": `version: "1"
tasks:
  build:
    dir: src
    cmd: go build .
  test:
    cmd: go test ./...
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(dir, "cli", "src"), tf.Tasks["cli:build"].Dir)
	assert.Equal(t, filepath.Join(dir, "cli"), tf.Tasks["cli:test"].Dir)
}

func TestLoadWithFlattenMergesTasksUnnamespaced(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
flatten:
  - tasks/lint.yml
  - tasks/test.yml
tasks:
  default:
    cmd: echo root
`,
		"tasks/lint.yml": `version: "1"
tasks:
  lint:backend:
    cmd: golangci-lint run
  lint:frontend:
    cmd: eslint .
`,
		"tasks/test.yml": `version: "1"
tasks:
  test:
    cmd: go test ./...
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	// Tasks land in the root namespace verbatim — colons in names are preserved.
	assert.Contains(t, tf.Tasks, "default")
	assert.Contains(t, tf.Tasks, "lint:backend")
	assert.Contains(t, tf.Tasks, "lint:frontend")
	assert.Contains(t, tf.Tasks, "test")

	assert.Equal(t, "golangci-lint run", tf.Tasks["lint:backend"].Cmds[0].Cmd)
	assert.Equal(t, "eslint .", tf.Tasks["lint:frontend"].Cmds[0].Cmd)
}

func TestLoadWithFlattenResolvesTaskDirAgainstRoot(t *testing.T) {
	// A `dir: backend` written inside a flatten file should mean "backend at
	// the project root", not relative to where the YAML file happens to live.
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
flatten:
  - tasks/utils.yml
`,
		"tasks/utils.yml": `version: "1"
tasks:
  build:
    dir: backend
    cmd: go build
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(dir, "backend"), tf.Tasks["build"].Dir)
}

func TestLoadWithFlattenRootTaskWinsCollision(t *testing.T) {
	// A task defined in the parent file beats a same-named task in a flatten file.
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
flatten:
  - tasks/extras.yml
tasks:
  build:
    cmd: from-root
`,
		"tasks/extras.yml": `version: "1"
tasks:
  build:
    cmd: from-flatten
  test:
    cmd: from-flatten
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	assert.Equal(t, "from-root", tf.Tasks["build"].Cmds[0].Cmd, "root task should win")
	assert.Equal(t, "from-flatten", tf.Tasks["test"].Cmds[0].Cmd)
}

func TestLoadWithFlattenFirstFlattenFileWinsCollision(t *testing.T) {
	// When two flatten files both define a task and the parent doesn't, the
	// first file listed wins (mirrors mergeVars).
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
flatten:
  - tasks/a.yml
  - tasks/b.yml
`,
		"tasks/a.yml": `version: "1"
tasks:
  build:
    cmd: from-a
`,
		"tasks/b.yml": `version: "1"
tasks:
  build:
    cmd: from-b
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	assert.Equal(t, "from-a", tf.Tasks["build"].Cmds[0].Cmd)
}

func TestLoadWithFlattenMergesGlobalVars(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
flatten:
  - tasks/extras.yml
vars:
  ROOT_ONLY: from-root
`,
		"tasks/extras.yml": `version: "1"
vars:
  FLAT_ONLY: from-flat
  ROOT_ONLY: should-lose
tasks:
  noop:
    cmd: "true"
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	assert.Equal(t, "from-root", tf.Vars["ROOT_ONLY"].Value, "root vars win conflicts")
	assert.Equal(t, "from-flat", tf.Vars["FLAT_ONLY"].Value, "flatten vars are merged in")
}

func TestLoadWithFlattenLocalTaskReferencesNotRewritten(t *testing.T) {
	// At root, a `task: foo` reference in a flattened file refers to the same
	// `foo` that's now in the root — no rewriting is needed or wanted.
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
flatten:
  - tasks/extras.yml
`,
		"tasks/extras.yml": `version: "1"
tasks:
  helper:
    cmd: helper
  build:
    cmds:
      - task: helper
      - go build
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	build := tf.Tasks["build"]
	require.Len(t, build.Cmds, 2)
	assert.Equal(t, "helper", build.Cmds[0].Task)
}

func TestLoadWithFlattenSupportsTaskComments(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
flatten:
  - tasks/lint.yml
`,
		"tasks/lint.yml": `version: "1"
tasks:
  # Run all linters
  lint:
    cmd: golangci-lint run
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	assert.Equal(t, "Run all linters", tf.Tasks["lint"].Desc)
}

func TestLoadWithFlattenNested(t *testing.T) {
	// A flatten file can itself flatten another file. All tasks land in root.
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
flatten:
  - tasks/a.yml
`,
		"tasks/a.yml": `version: "1"
flatten:
  - sub/b.yml
tasks:
  a-task:
    cmd: a
`,
		"tasks/sub/b.yml": `version: "1"
tasks:
  b-task:
    cmd: b
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	assert.Contains(t, tf.Tasks, "a-task")
	assert.Contains(t, tf.Tasks, "b-task")
}

func TestLoadWithFlattenRejectsCycles(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
flatten:
  - a.yml
`,
		"a.yml": `version: "1"
flatten:
  - b.yml
`,
		"b.yml": `version: "1"
flatten:
  - a.yml
`,
	})

	_, err := LoadWithIncludes(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cyclic flatten")
	assert.Contains(t, err.Error(), "a.yml")
}

func TestLoadWithFlattenMissingFile(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
flatten:
  - tasks/missing.yml
`,
	})

	_, err := LoadWithIncludes(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `loading flatten "tasks/missing.yml"`)
}

func TestLoadWithFlattenInsideIncludeUsesIncludeNamespace(t *testing.T) {
	// A flatten file pulled in from inside a namespaced include should have
	// its tasks land at that include's namespace, not at the root.
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
includes:
  - cli
`,
		"cli/gogo.yaml": `version: "1"
flatten:
  - helpers.yml
tasks:
  build:
    cmd: go build
`,
		"cli/helpers.yml": `version: "1"
tasks:
  helper:
    cmd: do-helper
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	assert.Contains(t, tf.Tasks, "cli:build")
	assert.Contains(t, tf.Tasks, "cli:helper", "flatten inside an include should use that include's namespace")
	assert.NotContains(t, tf.Tasks, "helper")
}

func TestLoadWithFlattenAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	extras := filepath.Join(dir, "shared", "extras.yml")
	writeFiles(t, dir, map[string]string{
		"shared/extras.yml": `version: "1"
tasks:
  shared-task:
    cmd: echo shared
`,
	})
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gogo.yaml"), fmt.Appendf(nil, `version: "1"
flatten:
  - %s
`, extras), 0o644))

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	assert.Contains(t, tf.Tasks, "shared-task")
}

func TestParseFlattenField(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gogo.yaml"), []byte(`version: "1"
flatten:
  - tasks/a.yml
  - tasks/b.yml
`), 0o644))

	tf, err := Parse(dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"tasks/a.yml", "tasks/b.yml"}, tf.Flatten)
}
