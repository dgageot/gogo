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
	writeFiles(t, dir, map[string]string{
		"a.txt": "hello",
		"b.txt": "world",
	})

	sum1, err := sourcesChecksum(dir, []string{"*.txt"})
	require.NoError(t, err)
	assert.NotEmpty(t, sum1)

	// Same content produces same checksum
	sum2, err := sourcesChecksum(dir, []string{"*.txt"})
	require.NoError(t, err)
	assert.Equal(t, sum1, sum2)

	// Changing content changes checksum
	writeFiles(t, dir, map[string]string{"a.txt": "changed"})

	sum3, err := sourcesChecksum(dir, []string{"*.txt"})
	require.NoError(t, err)
	assert.NotEqual(t, sum1, sum3)
}

func TestSourcesChecksumNoMatches(t *testing.T) {
	dir := t.TempDir()

	sum, err := sourcesChecksum(dir, []string{"*.go"})
	require.NoError(t, err)
	assert.Empty(t, sum, "no matches should return empty checksum")
}

func TestSourcesChecksumRecursive(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"root.go":          "package main",
		"sub/lib.go":       "package lib",
		"sub/deep/deep.go": "package deep",
		"sub/skip.txt":     "not go",
	})

	sum, err := sourcesChecksum(dir, []string{"**/*.go"})
	require.NoError(t, err)
	assert.NotEmpty(t, sum)

	// Changing a deeply nested file changes the checksum
	oldSum := sum
	writeFiles(t, dir, map[string]string{"sub/deep/deep.go": "package deep2"})

	sum, err = sourcesChecksum(dir, []string{"**/*.go"})
	require.NoError(t, err)
	assert.NotEqual(t, oldSum, sum)
}

func TestSourcesChecksumSkipsGitDir(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"main.go":             "package main",
		".git/objects/abc.go": "git object",
	})

	sum1, err := sourcesChecksum(dir, []string{"**/*.go"})
	require.NoError(t, err)

	// Adding a file inside .git should not change the checksum
	writeFiles(t, dir, map[string]string{".git/objects/def.go": "another"})

	sum2, err := sourcesChecksum(dir, []string{"**/*.go"})
	require.NoError(t, err)
	assert.Equal(t, sum1, sum2)
}

func TestSourcesChecksumMixedPatterns(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"go.mod":     "module test",
		"main.go":    "package main",
		"sub/lib.go": "package lib",
	})

	sum, err := sourcesChecksum(dir, []string{"**/*.go", "go.mod"})
	require.NoError(t, err)
	assert.NotEmpty(t, sum)

	// Changing go.mod changes the checksum
	oldSum := sum
	writeFiles(t, dir, map[string]string{"go.mod": "module test2"})

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

	// No outputs yet -> not up-to-date
	writeFiles(t, dir, map[string]string{"main.go": "package main"})
	upToDate, err := outputsNewerThanSources(dir, []string{"*.go"}, []string{"main"})
	require.NoError(t, err)
	assert.False(t, upToDate)

	// Create output newer than source -> up-to-date
	writeFiles(t, dir, map[string]string{"main": "binary"})
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
	writeFiles(t, dir, map[string]string{
		"main.go": "package main",
		"main":    "binary",
	})

	now := time.Now().Add(time.Second)
	require.NoError(t, os.Chtimes(src, now, now))

	upToDate, err := outputsNewerThanSources(dir, []string{"*.go"}, []string{"main", "*.go"})
	require.NoError(t, err)
	assert.False(t, upToDate, "sources listed in generates must not mask stale outputs")
}

func TestRecursivePatternWithSubdir(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"root.go":         "root",
		"sub/a.go":        "a",
		"sub/nested/b.go": "b",
		"other/c.go":      "c",
	})

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
	writeFiles(t, dir, map[string]string{"output": "data"})

	// Output exists but no sources match -> should not be considered up-to-date
	upToDate, err := outputsNewerThanSources(dir, []string{"*.go"}, []string{"output"})
	require.NoError(t, err)
	assert.False(t, upToDate, "should not be up-to-date when no sources match")
}
