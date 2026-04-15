package taskfile

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
		d := fileDigest(f)
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

	type recurPattern struct {
		baseDir  string
		filePart string
	}

	var recurPatterns []recurPattern
	var simplePatterns []string

	for _, pattern := range patterns {
		if before, after, ok := strings.Cut(pattern, "**"); ok {
			baseDir := strings.TrimRight(before, string(filepath.Separator))
			filePart := strings.TrimLeft(after, string(filepath.Separator))
			if filePart == "" {
				filePart = "*"
			}
			recurPatterns = append(recurPatterns, recurPattern{
				baseDir:  baseDir,
				filePart: filePart,
			})
		} else {
			simplePatterns = append(simplePatterns, pattern)
		}
	}

	var files []string

	// Group recursive patterns by base directory
	for _, rp := range recurPatterns {
		baseDir := dir
		if rp.baseDir != "" {
			baseDir = filepath.Join(dir, rp.baseDir)
		}
		files = append(files, walkRecursive(baseDir, []string{rp.filePart})...)
	}

	for _, pattern := range simplePatterns {
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(dir, pattern)
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		files = append(files, matches...)
	}

	return files, nil
}

// walkRecursive walks dir recursively and returns files matching any pattern.
// Hidden directories are skipped.
func walkRecursive(dir string, patterns []string) []string {
	var files []string

	var walk func(string)
	walk = func(dirPath string) {
		entries, err := os.ReadDir(dirPath)
		if err != nil {
			return
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() {
				if !strings.HasPrefix(name, ".") {
					walk(filepath.Join(dirPath, name))
				}
				continue
			}
			if slices.ContainsFunc(patterns, func(p string) bool {
				matched, _ := filepath.Match(p, name)
				return matched
			}) {
				files = append(files, filepath.Join(dirPath, name))
			}
		}
	}

	walk(dir)
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
