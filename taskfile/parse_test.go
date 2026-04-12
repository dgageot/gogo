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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gogo.yaml"), []byte(`version: "1"
includes:
  - cli
tasks:
  todo:
    cmd: open TODO.md
`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cli"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cli", "gogo.yaml"), []byte(`version: "1"
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
`), 0o644))

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
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cli", "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gogo.yaml"), []byte(`version: "1"
includes:
  - cli
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cli", "gogo.yaml"), []byte(`version: "1"
includes:
  - nested
tasks:
  build:
    cmd: go build .
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cli", "nested", "gogo.yaml"), []byte(`version: "1"
tasks:
  test:
    cmd: go test ./...
`), 0o644))

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	assert.Contains(t, tf.Tasks, "cli:build")
	assert.Contains(t, tf.Tasks, "nested:test")
}

func TestLoadWithIncludesRejectsCycles(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cli", "root"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gogo.yaml"), []byte(`version: "1"
includes:
  - cli
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cli", "gogo.yaml"), []byte(`version: "1"
includes:
  - root
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cli", "root", "gogo.yaml"), []byte(`version: "1"
includes:
  - ../..
`), 0o644))

	_, err := LoadWithIncludes(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cyclic include detected")
}

func TestExpandTemplates(t *testing.T) {
	t.Setenv("MY_VAR", "hello")

	assert.Equal(t, []byte("value: hello"), expandTemplates([]byte("value: {{.MY_VAR}}")))
	assert.Equal(t, []byte("value: hello"), expandTemplates([]byte("value: {{ .MY_VAR }}")))
	assert.Equal(t, []byte("value: {{.UNSET_VAR}}"), expandTemplates([]byte("value: {{.UNSET_VAR}}")))
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
