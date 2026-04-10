package taskfile

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// sourcesChecksum computes a SHA256 checksum of all files matching the given
// glob patterns, resolved relative to dir.
func sourcesChecksum(dir string, patterns []string) (string, error) {
	files, err := globFiles(dir, patterns)
	if err != nil {
		return "", err
	}

	h := sha256.New()
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue // skip directories and unreadable files
		}
		fmt.Fprintf(h, "%s\n", f)
		h.Write(data)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// globFiles expands glob patterns relative to dir, returning a sorted, deduplicated file list.
func globFiles(dir string, patterns []string) ([]string, error) {
	var files []string
	for _, pattern := range patterns {
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(dir, pattern)
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("glob %q: %w", pattern, err)
		}
		files = append(files, matches...)
	}

	slices.Sort(files)
	return slices.Compact(files), nil
}

// checksumPath returns the file path for a task's stored checksum.
func checksumPath(taskfileDir, taskName string) string {
	safeName := strings.ReplaceAll(taskName, ":", "_")
	return filepath.Join(taskfileDir, ".task", "checksum", safeName)
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
	path := checksumPath(taskfileDir, taskName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(checksum), 0o644)
}
