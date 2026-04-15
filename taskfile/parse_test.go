package taskfile

import (
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
	assert.Contains(t, err.Error(), "cyclic include detected")
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

	tf := &Taskfile{
		Tasks: map[string]Task{
			"build":  {Cmd: Cmd{Cmd: "go build"}},
			"test":   {Cmd: Cmd{Cmd: "go test"}},
			"deploy": {Cmd: Cmd{Cmd: "deploy.sh"}},
		},
	}

	applyTaskComments(tf, yamlData)

	assert.Equal(t, "Build the project", tf.Tasks["build"].Desc)
	assert.Equal(t, "Run all the tests", tf.Tasks["test"].Desc)
	assert.Empty(t, tf.Tasks["deploy"].Desc)
}
