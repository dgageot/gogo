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
	path := filepath.Join(dir, ".env")

	content := `# comment
KEY1=value1
KEY2="quoted value"
KEY3='single quoted'
KEY4=

SPACED_KEY = spaced_value
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	env, err := parseDotenv(path)
	require.NoError(t, err)

	assert.Equal(t, "value1", env["KEY1"])
	assert.Equal(t, "quoted value", env["KEY2"])
	assert.Equal(t, "single quoted", env["KEY3"])
	assert.Equal(t, "", env["KEY4"])
	assert.Equal(t, "spaced_value", env["SPACED_KEY"])
}

func TestParseDotenvMissingFile(t *testing.T) {
	_, err := parseDotenv("/nonexistent/.env")
	assert.Error(t, err)
}

func TestLoadDotenvFilesDeduplication(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(path, []byte("KEY=value\n"), 0o644))

	seen := make(map[string]struct{})

	env1, err := loadDotenvFiles(dir, []string{".env"}, seen)
	require.NoError(t, err)
	assert.Equal(t, "value", env1["KEY"])

	// Loading the same file again returns nothing
	env2, err := loadDotenvFiles(dir, []string{".env"}, seen)
	require.NoError(t, err)
	assert.Empty(t, env2)
}

func TestLoadDotenvFilesSkipsMissing(t *testing.T) {
	dir := t.TempDir()
	seen := make(map[string]struct{})

	env, err := loadDotenvFiles(dir, []string{"nonexistent.env"}, seen)
	require.NoError(t, err)
	assert.Empty(t, env)
}

func TestUnquote(t *testing.T) {
	assert.Equal(t, "hello", unquote(`"hello"`))
	assert.Equal(t, "hello", unquote(`'hello'`))
	assert.Equal(t, "hello", unquote("hello"))
	assert.Equal(t, "", unquote(`""`))
	assert.Equal(t, "", unquote(`''`))
	assert.Equal(t, "a", unquote("a"))
}
