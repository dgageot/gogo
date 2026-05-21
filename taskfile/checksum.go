package taskfile

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
		// Mix in the file's path *relative to dir*, with forward slashes,
		// so identical trees produce identical checksums regardless of the
		// absolute location on disk (developer laptop vs. CI workspace).
		identity := relIdentity(dir, f)
		d, err := fileDigest(f, identity)
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

// relIdentity returns the path of f relative to dir, normalised to forward
// slashes. If a relative path can't be computed (e.g. f is on a different
// volume on Windows) the absolute path is returned as a safe fallback —
// the checksum stays consistent on the same machine, just not portable.
func relIdentity(dir, f string) string {
	rel, err := filepath.Rel(dir, f)
	if err != nil {
		return filepath.ToSlash(f)
	}
	return filepath.ToSlash(rel)
}

// checksumPath returns the file path for a task's stored checksum.
func checksumPath(fileDir, taskName string) string {
	return filepath.Join(fileDir, ".gogo", "checksum", sanitizeTaskName(taskName))
}

// sanitizeTaskName encodes a task name as a filesystem-safe filename using
// a reversible escape: '_' -> '__' and ':' -> '_.'. Any two distinct task
// names produce distinct encodings, so separate tasks cannot share a file.
func sanitizeTaskName(name string) string {
	escaped := strings.ReplaceAll(name, "_", "__")
	return strings.ReplaceAll(escaped, ":", "_.")
}

// readStoredChecksum returns the previously stored checksum for a task, or empty if none.
func readStoredChecksum(fileDir, taskName string) string {
	data, err := os.ReadFile(checksumPath(fileDir, taskName))
	if err != nil {
		return ""
	}
	return string(data)
}

// writeChecksum stores the checksum for a task. The write is hardened
// against local symlink attacks: we remove any pre-existing entry (without
// following symlinks) and then re-create the file with O_CREATE|O_EXCL so
// a pre-placed symlink in .gogo/checksum/ can't redirect the write
// elsewhere on disk — if an attacker wins the race and recreates a
// symlink between the Remove and the OpenFile, the OpenFile call fails.
func writeChecksum(fileDir, taskName, checksum string) error {
	p := checksumPath(fileDir, taskName)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	// os.Remove does not follow symlinks, so we only ever delete the entry
	// itself, never its target.
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(checksum); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
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
