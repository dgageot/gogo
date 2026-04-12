package taskfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestOutputsNewerThanSources(t *testing.T) {
	dir := t.TempDir()

	src := filepath.Join(dir, "main.go")
	out := filepath.Join(dir, "main")

	// No outputs yet -> not up-to-date
	require.NoError(t, os.WriteFile(src, []byte("package main"), 0o644))
	upToDate, err := outputsNewerThanSources(dir, []string{"*.go"}, []string{"main"})
	require.NoError(t, err)
	assert.False(t, upToDate)

	// Create output newer than source -> up-to-date
	require.NoError(t, os.WriteFile(out, []byte("binary"), 0o644))
	upToDate, err = outputsNewerThanSources(dir, []string{"*.go"}, []string{"main"})
	require.NoError(t, err)
	assert.True(t, upToDate)

	// Touch source to be newer -> not up-to-date
	now := time.Now().Add(time.Second)
	require.NoError(t, os.Chtimes(src, now, now))
	upToDate, err = outputsNewerThanSources(dir, []string{"*.go"}, []string{"main"})
	require.NoError(t, err)
	assert.False(t, upToDate)
}

func TestOutputsNewerThanSourcesWithSourcesAlsoListedAsOutputs(t *testing.T) {
	dir := t.TempDir()

	src := filepath.Join(dir, "main.go")
	out := filepath.Join(dir, "main")

	require.NoError(t, os.WriteFile(src, []byte("package main"), 0o644))
	require.NoError(t, os.WriteFile(out, []byte("binary"), 0o644))

	now := time.Now().Add(time.Second)
	require.NoError(t, os.Chtimes(src, now, now))

	upToDate, err := outputsNewerThanSources(dir, []string{"*.go"}, []string{"main", "*.go"})
	require.NoError(t, err)
	assert.False(t, upToDate, "sources listed in generates must not mask stale outputs")
}

func TestRecursivePatternWithSubdir(t *testing.T) {
	dir := t.TempDir()

	// Create files in sub/ and other/
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub", "nested"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "other"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "root.go"), []byte("root"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "a.go"), []byte("a"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "nested", "b.go"), []byte("b"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other", "c.go"), []byte("c"), 0o644))

	// sub/**/*.go should only match files under sub/
	files, err := discoverFiles(dir, []string{"sub/**/*.go"})
	require.NoError(t, err)

	for _, f := range files {
		rel, _ := filepath.Rel(dir, f)
		assert.True(t, strings.HasPrefix(rel, "sub"),
			"file %q should be under sub/", rel)
	}
	assert.Len(t, files, 2)
}

func TestOutputsNewerThanSourcesNoSources(t *testing.T) {
	dir := t.TempDir()

	// Output exists but no sources match -> should not be considered up-to-date
	require.NoError(t, os.WriteFile(filepath.Join(dir, "output"), []byte("data"), 0o644))
	upToDate, err := outputsNewerThanSources(dir, []string{"*.go"}, []string{"output"})
	require.NoError(t, err)
	assert.False(t, upToDate, "should not be up-to-date when no sources match")
}
