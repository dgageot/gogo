package taskfile

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	tf, err := LoadWithIncludes("/Users/dgageot/src/ai")
	require.NoError(t, err)

	assert.Equal(t, "1", tf.Version)
	assert.NotEmpty(t, tf.Tasks)

	// Root task
	task, ok := tf.Tasks["todo"]
	assert.True(t, ok)
	assert.Equal(t, "Open our shared TODO list", task.Desc)

	// Included task
	task, ok = tf.Tasks["cli:build"]
	assert.True(t, ok)
	assert.Equal(t, "Build the docker-ai CLI", task.Desc)

	// Task with deps
	task, ok = tf.Tasks["cli:install"]
	assert.True(t, ok)
	assert.Len(t, task.Deps, 1)
	assert.Equal(t, "build", task.Deps[0].Task)

	// Task with aliases
	task, ok = tf.Tasks["github"]
	assert.True(t, ok)
	assert.Equal(t, []string{"gh"}, task.Aliases)
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
	assert.Equal(t, "", tf.Tasks["deploy"].Desc)
}
