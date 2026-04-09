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
	var files []string
	for _, pattern := range patterns {
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(dir, pattern)
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return "", fmt.Errorf("glob %q: %w", pattern, err)
		}
		files = append(files, matches...)
	}

	slices.Sort(files)
	files = slices.Compact(files)

	h := sha256.New()
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			return "", err
		}
		if info.IsDir() {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			return "", err
		}
		h.Write([]byte(f))
		h.Write([]byte{'\n'})
		h.Write(data)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// checksumPath returns the file path for a task's stored checksum.
func checksumPath(taskfileDir, taskName string) string {
	safeName := strings.ReplaceAll(taskName, ":", "_")
	return filepath.Join(taskfileDir, ".task", "checksum", safeName)
}
func readStoredChecksum(taskfileDir, taskName string) string {
	data, err := os.ReadFile(checksumPath(taskfileDir, taskName))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// writeChecksum stores the checksum for a task.
func writeChecksum(taskfileDir, taskName, checksum string) error {
	path := checksumPath(taskfileDir, taskName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(checksum+"\n"), 0o644)
}

