package taskfile

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
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

	// Hash each file in parallel
	digests := make([][sha256.Size]byte, len(files))
	var wg sync.WaitGroup
	for i, f := range files {
		wg.Go(func() {
			digests[i] = fileDigest(f)
		})
	}
	wg.Wait()

	// Combine per-file digests
	h := sha256.New()
	for _, d := range digests {
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

	var recurPatterns []string
	var simplePatterns []string

	for _, pattern := range patterns {
		if strings.Contains(pattern, "**") {
			_, filePart := filepath.Split(pattern)
			recurPatterns = append(recurPatterns, filePart)
		} else {
			simplePatterns = append(simplePatterns, pattern)
		}
	}

	var files []string

	if len(recurPatterns) > 0 {
		files = walkRecursive(dir, recurPatterns)
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

// walkRecursive walks dir in parallel and returns files matching any pattern.
// Hidden directories are skipped.
func walkRecursive(dir string, patterns []string) []string {
	var mu sync.Mutex
	var files []string
	var wg sync.WaitGroup

	var walk func(string)
	walk = func(dirPath string) {
		entries, err := os.ReadDir(dirPath)
		if err != nil {
			return
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() {
				if strings.HasPrefix(name, ".") {
					continue
				}
				wg.Go(func() { walk(filepath.Join(dirPath, name)) })
				continue
			}
			for _, p := range patterns {
				matched, _ := filepath.Match(p, name)
				if !matched {
					continue
				}
				mu.Lock()
				files = append(files, filepath.Join(dirPath, name))
				mu.Unlock()
				break
			}
		}
	}

	walk(dir)
	wg.Wait()
	return files
}

// checksumPath returns the file path for a task's stored checksum.
func checksumPath(taskfileDir, taskName string) string {
	safeName := strings.ReplaceAll(taskName, ":", "_")
	return filepath.Join(taskfileDir, ".gogo", "checksum", safeName)
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
