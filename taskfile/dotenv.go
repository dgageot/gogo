package taskfile

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
)

// loadDotenvFiles reads the given .env file paths, resolving them relative to dir.
// It skips files that don't exist. Already-seen absolute paths (tracked via seen)
// are skipped to avoid loading the same file twice across included Taskfiles.
func loadDotenvFiles(dir string, paths []string, seen map[string]struct{}) (map[string]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	result := make(map[string]string)

	for _, p := range paths {
		abs := resolvePath(dir, p)

		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}

		vars, err := parseDotenv(abs)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", abs, err)
		}

		maps.Copy(result, vars)
	}

	return result, nil
}

// parseDotenv reads a .env file and returns key-value pairs.
// It supports blank lines, comments (#), and simple KEY=VALUE pairs.
// Quoted values (single or double) are unquoted.
func parseDotenv(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	vars := make(map[string]string)
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if key, value, ok := strings.Cut(line, "="); ok {
			vars[strings.TrimSpace(key)] = unquote(strings.TrimSpace(value))
		}
	}

	return vars, nil
}

// unquote removes matching surrounding quotes from a value.
func unquote(s string) string {
	for _, q := range []string{`"`, `'`} {
		if after, ok := strings.CutPrefix(s, q); ok {
			if before, ok := strings.CutSuffix(after, q); ok {
				return before
			}
		}
	}
	return s
}

// resolvePath resolves a path relative to dir, expanding ~ to the home directory.
func resolvePath(dir, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	if after, ok := strings.CutPrefix(p, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, after)
		}
	}
	return filepath.Join(dir, p)
}
