package taskfile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSourcesChecksum(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("world"), 0o644))

	sum1, err := sourcesChecksum(dir, []string{"*.txt"})
	require.NoError(t, err)
	assert.NotEmpty(t, sum1)

	// Same content produces same checksum
	sum2, err := sourcesChecksum(dir, []string{"*.txt"})
	require.NoError(t, err)
	assert.Equal(t, sum1, sum2)

	// Changing content changes checksum
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed"), 0o644))

	sum3, err := sourcesChecksum(dir, []string{"*.txt"})
	require.NoError(t, err)
	assert.NotEqual(t, sum1, sum3)
}

func TestSourcesChecksumNoMatches(t *testing.T) {
	dir := t.TempDir()

	sum, err := sourcesChecksum(dir, []string{"*.go"})
	require.NoError(t, err)
	assert.NotEmpty(t, sum, "empty file set should still produce a checksum")
}

func TestSourcesChecksumRecursive(t *testing.T) {
	dir := t.TempDir()

	// Create nested structure
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub", "deep"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "root.go"), []byte("package main"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "lib.go"), []byte("package lib"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "deep", "deep.go"), []byte("package deep"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "skip.txt"), []byte("not go"), 0o644))

	sum, err := sourcesChecksum(dir, []string{"**/*.go"})
	require.NoError(t, err)
	assert.NotEmpty(t, sum)

	// Changing a deeply nested file changes the checksum
	oldSum := sum
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "deep", "deep.go"), []byte("package deep2"), 0o644))

	sum, err = sourcesChecksum(dir, []string{"**/*.go"})
	require.NoError(t, err)
	assert.NotEqual(t, oldSum, sum)
}

func TestSourcesChecksumSkipsGitDir(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git", "objects"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git", "objects", "abc.go"), []byte("git object"), 0o644))

	sum1, err := sourcesChecksum(dir, []string{"**/*.go"})
	require.NoError(t, err)

	// Adding a file inside .git should not change the checksum
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git", "objects", "def.go"), []byte("another"), 0o644))

	sum2, err := sourcesChecksum(dir, []string{"**/*.go"})
	require.NoError(t, err)
	assert.Equal(t, sum1, sum2)
}

func TestSourcesChecksumMixedPatterns(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "lib.go"), []byte("package lib"), 0o644))

	sum, err := sourcesChecksum(dir, []string{"**/*.go", "go.mod"})
	require.NoError(t, err)
	assert.NotEmpty(t, sum)

	// Changing go.mod changes the checksum
	oldSum := sum
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test2"), 0o644))

	sum, err = sourcesChecksum(dir, []string{"**/*.go", "go.mod"})
	require.NoError(t, err)
	assert.NotEqual(t, oldSum, sum)
}

func TestChecksumStorage(t *testing.T) {
	dir := t.TempDir()

	assert.Empty(t, readStoredChecksum(dir, "build"))

	require.NoError(t, writeChecksum(dir, "build", "abc123"))
	assert.Equal(t, "abc123", readStoredChecksum(dir, "build"))

	// Colons in task names are sanitized
	require.NoError(t, writeChecksum(dir, "cli:build", "def456"))
	assert.Equal(t, "def456", readStoredChecksum(dir, "cli:build"))

	// Verify file is named with underscore
	_, err := os.Stat(filepath.Join(dir, ".gogo", "checksum", "cli_build"))
	assert.NoError(t, err)
}
