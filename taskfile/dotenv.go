package taskfile

import (
	"bufio"
	"maps"
	"os"
	"path/filepath"
	"strings"
)

// loadDotenvFiles reads the given .env file paths, resolving them relative to dir.
// It skips files that don't exist. Already-seen absolute paths (tracked via seen)
// are skipped to avoid loading the same file twice across included Taskfiles.
func loadDotenvFiles(dir string, paths []string, seen map[string]struct{}) (map[string]string, error) {
	env := make(map[string]string)

	for _, p := range paths {
		switch {
		case strings.HasPrefix(p, "~/"):
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, err
			}
			p = filepath.Join(home, p[2:])
		case !filepath.IsAbs(p):
			p = filepath.Join(dir, p)
		}

		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}

		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}

		vars, err := parseDotenv(abs)
		if err != nil {
			continue // skip missing files
		}

		maps.Copy(env, vars)
	}

	return env, nil
}

// parseDotenv reads a .env file and returns key-value pairs.
// It supports blank lines, comments (#), and simple KEY=VALUE pairs.
// Quoted values (single or double) are unquoted.
func parseDotenv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	env := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = unquote(value)

		env[key] = value
	}

	return env, scanner.Err()
}

// unquote removes matching surrounding quotes from a value.
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
