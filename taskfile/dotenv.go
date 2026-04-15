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
	result := make(map[string]string)

	if len(paths) == 0 {
		return result, nil
	}

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

		// Strip optional "export " prefix
		line = strings.TrimPrefix(line, "export ")

		if key, value, ok := strings.Cut(line, "="); ok {
			key = strings.TrimSpace(key)
			if !isValidEnvKey(key) {
				return nil, fmt.Errorf("invalid dotenv key %q", key)
			}
			vars[key] = unquote(strings.TrimSpace(value))
		}
	}

	return vars, nil
}

func isValidEnvKey(key string) bool {
	if key == "" {
		return false
	}

	for i, r := range key {
		switch {
		case r == '_' || ('A' <= r && r <= 'Z') || ('a' <= r && r <= 'z'):
		case i > 0 && '0' <= r && r <= '9':
		default:
			return false
		}
	}

	return true
}

// unquote removes matching surrounding quotes from a value and processes escape sequences.
// Double-quoted values support \" and \\ escapes. Single-quoted values are literal.
func unquote(s string) string {
	if after, ok := strings.CutPrefix(s, `"`); ok {
		if before, ok := strings.CutSuffix(after, `"`); ok {
			// Process escape sequences in double-quoted strings
			r := strings.NewReplacer(`\"`, `"`, `\\`, `\`, `\n`, "\n", `\t`, "\t")
			return r.Replace(before)
		}
	}
	if after, ok := strings.CutPrefix(s, "'"); ok {
		if before, ok := strings.CutSuffix(after, "'"); ok {
			return before
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
