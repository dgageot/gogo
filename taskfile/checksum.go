package taskfile

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// sourcesChecksum computes a SHA256 checksum of all files matching the given
// glob patterns, resolved relative to dir.
//
// Patterns containing "**" are matched recursively. Hidden directories
// (starting with '.') are skipped during recursive traversal.
func sourcesChecksum(dir string, patterns []string) (string, error) {
	files, err := discoverFiles(dir, patterns)
	if err != nil {
		return "", err
	}

	slices.Sort(files)
	files = slices.Compact(files)

	// No files matched: return empty to signal "not up to date".
	if len(files) == 0 {
		return "", nil
	}

	h := sha256.New()
	for _, f := range files {
		d, err := fileDigest(f)
		if err != nil {
			return "", err
		}
		h.Write(d[:])
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// discoverFiles finds all files matching the given glob patterns.
// Patterns containing "**" are matched recursively; others use filepath.Glob.
func discoverFiles(dir string, patterns []string) ([]string, error) {
	if len(patterns) == 0 {
		return nil, nil
	}

	var files []string
	for _, pattern := range patterns {
		if before, after, ok := strings.Cut(pattern, "**"); ok {
			files = append(files, matchRecursivePattern(dir, before, after)...)
		} else {
			matched, err := matchSimplePattern(dir, pattern)
			if err != nil {
				return nil, err
			}
			files = append(files, matched...)
		}
	}

	return files, nil
}

// matchRecursivePattern handles a single "**" glob pattern.
// before is the path prefix before "**", after is the suffix.
func matchRecursivePattern(dir, before, after string) []string {
	baseDir := dir
	if prefix := strings.TrimRight(before, string(filepath.Separator)); prefix != "" {
		baseDir = filepath.Join(dir, prefix)
	}

	filePart := strings.TrimLeft(after, string(filepath.Separator))
	if filePart == "" {
		filePart = "*"
	}

	return walkRecursive(baseDir, filePart)
}

// matchSimplePattern handles a single non-recursive glob pattern.
func matchSimplePattern(dir, pattern string) ([]string, error) {
	if !filepath.IsAbs(pattern) {
		pattern = filepath.Join(dir, pattern)
	}
	return filepath.Glob(pattern)
}

// walkRecursive walks dir and returns all files whose base name matches pattern.
// Hidden directories (starting with '.') are skipped.
func walkRecursive(dir, pattern string) []string {
	var files []string
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != dir && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if matched, _ := filepath.Match(pattern, d.Name()); matched {
			files = append(files, path)
		}
		return nil
	})
	return files
}

// checksumPath returns the file path for a task's stored checksum.
func checksumPath(taskfileDir, taskName string) string {
	return filepath.Join(taskfileDir, ".gogo", "checksum", strings.ReplaceAll(taskName, ":", "_"))
}

// readStoredChecksum returns the previously stored checksum for a task, or empty if none.
func readStoredChecksum(taskfileDir, taskName string) string {
	data, err := os.ReadFile(checksumPath(taskfileDir, taskName))
	if err != nil {
		return ""
	}
	return string(data)
}

// writeChecksum stores the checksum for a task.
func writeChecksum(taskfileDir, taskName, checksum string) error {
	p := checksumPath(taskfileDir, taskName)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(checksum), 0o644)
}

// outputsNewerThanSources checks if all generated files exist and are newer
// than all source files. Returns true only when every output is up-to-date.
func outputsNewerThanSources(dir string, sourcePatterns, generatePatterns []string) (bool, error) {
	sources, err := discoverFiles(dir, sourcePatterns)
	if err != nil {
		return false, fmt.Errorf("discovering sources: %w", err)
	}

	generatedOutputs, err := discoverFiles(dir, generatePatterns)
	if err != nil {
		return false, fmt.Errorf("discovering outputs: %w", err)
	}

	// If no outputs exist yet, the task must run
	if len(generatedOutputs) == 0 {
		return false, nil
	}

	// If no sources matched, always run (can't determine freshness)
	if len(sources) == 0 {
		return false, nil
	}

	// Find the newest source modification time
	var newestSource time.Time
	for _, f := range sources {
		info, err := os.Stat(f)
		if err != nil {
			return false, err
		}
		if t := info.ModTime(); t.After(newestSource) {
			newestSource = t
		}
	}

	// Check that every output exists and is newer than the newest source
	for _, f := range generatedOutputs {
		info, err := os.Stat(f)
		if err != nil {
			return false, nil //nolint:nilerr // missing output means not up-to-date
		}
		if !info.ModTime().After(newestSource) {
			return false, nil
		}
	}

	return true, nil
}
