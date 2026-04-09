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
		fmt.Fprintf(h, "%s\n%d\n", f, len(data))
		h.Write(data)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// checksumDir returns the directory where task checksums are stored.
func checksumDir(taskfileDir string) string {
	return filepath.Join(taskfileDir, ".task", "checksum")
}

// readStoredChecksum reads the previously stored checksum for a task.
func readStoredChecksum(taskfileDir, taskName string) string {
	data, err := os.ReadFile(filepath.Join(checksumDir(taskfileDir), sanitizeTaskName(taskName)))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// writeChecksum stores the checksum for a task.
func writeChecksum(taskfileDir, taskName, checksum string) error {
	dir := checksumDir(taskfileDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, sanitizeTaskName(taskName)), []byte(checksum+"\n"), 0o644)
}

// sanitizeTaskName replaces characters that are not safe for filenames.
func sanitizeTaskName(name string) string {
	return strings.ReplaceAll(name, ":", "_")
}
