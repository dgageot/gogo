package taskfile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDotenv(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		".env": `# comment
KEY1=value1
KEY2="quoted value"
KEY3='single quoted'
KEY4=

SPACED_KEY = spaced_value
`,
	})

	env, err := parseDotenv(filepath.Join(dir, ".env"))
	require.NoError(t, err)

	assert.Equal(t, "value1", env["KEY1"])
	assert.Equal(t, "quoted value", env["KEY2"])
	assert.Equal(t, "single quoted", env["KEY3"])
	assert.Empty(t, env["KEY4"])
	assert.Equal(t, "spaced_value", env["SPACED_KEY"])
}

func TestParseDotenvRejectsInvalidKey(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{".env": "BAD-KEY=value\n"})

	_, err := parseDotenv(filepath.Join(dir, ".env"))
	require.EqualError(t, err, `invalid dotenv key "BAD-KEY"`)
}

func TestParseDotenvMissingFile(t *testing.T) {
	_, err := parseDotenv("/nonexistent/.env")
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestLoadDotenvFilesDeduplication(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{".env": "KEY=value\n"})

	seen := make(map[string]struct{})

	env1, err := loadDotenvFiles(dir, []string{".env"}, seen)
	require.NoError(t, err)
	assert.Equal(t, "value", env1["KEY"])

	// Loading the same file again returns nothing
	env2, err := loadDotenvFiles(dir, []string{".env"}, seen)
	require.NoError(t, err)
	assert.Empty(t, env2)
}

func TestLoadDotenvFilesHomeTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	path := filepath.Join(home, ".gogo-test-dotenv")
	require.NoError(t, os.WriteFile(path, []byte("TILDE_KEY=tilde_value\n"), 0o644))
	t.Cleanup(func() { os.Remove(path) })

	seen := make(map[string]struct{})
	env, err := loadDotenvFiles("/unused", []string{"~/.gogo-test-dotenv"}, seen)
	require.NoError(t, err)
	assert.Equal(t, "tilde_value", env["TILDE_KEY"])
}

func TestLoadDotenvFilesSkipsMissing(t *testing.T) {
	dir := t.TempDir()
	seen := make(map[string]struct{})

	env, err := loadDotenvFiles(dir, []string{"nonexistent.env"}, seen)
	require.NoError(t, err)
	assert.Empty(t, env)
}

func TestBuildEnvWithTaskDotenv(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{".env.task": "TASK_VAR=task_value\n"})

	tf := &Taskfile{Dir: dir, Tasks: make(map[string]Task), DotenvVars: make(map[string]string)}
	r := NewRunner(tf, dir)

	task := &Task{Dotenv: []string{".env.task"}}
	env, err := r.buildEnv(task, dir, nil)
	require.NoError(t, err)
	assert.Contains(t, env, "TASK_VAR=task_value")
}

func TestBuildEnvWithoutTaskDotenv(t *testing.T) {
	tf := &Taskfile{Dir: t.TempDir(), Tasks: make(map[string]Task), DotenvVars: make(map[string]string)}
	r := NewRunner(tf, tf.Dir)

	task := &Task{}
	env, err := r.buildEnv(task, tf.Dir, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, env) // at least inherited env vars
}

func TestUnquote(t *testing.T) {
	assert.Equal(t, "hello", unquote(`"hello"`))
	assert.Equal(t, "hello", unquote(`'hello'`))
	assert.Equal(t, "hello", unquote("hello"))
	assert.Empty(t, unquote(`""`))
	assert.Empty(t, unquote(`''`))
	assert.Equal(t, "a", unquote("a"))
	assert.Equal(t, `"hello'`, unquote(`"hello'`))
	assert.Equal(t, `'hello"`, unquote(`'hello"`))
}
