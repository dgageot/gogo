package taskfile

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	tf, err := LoadWithIncludes("/Users/dgageot/src/ai")
	require.NoError(t, err)

	assert.Equal(t, "3", tf.Version)
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
