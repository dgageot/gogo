package taskfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	yaml "github.com/goccy/go-yaml"
)

const fileName = "gogo.yaml"

// validateTaskName enforces a strict whitelist of characters allowed in task
// names. Names appear in checksum file paths, log lines, and shell
// completions; allowing arbitrary characters lets a malicious task file
// inject control characters, terminal escapes, path components, or shell
// metacharacters into those contexts.
//
// Allowed: ASCII letters, digits, '-', '_', and ':' (the namespace separator).
func validateTaskName(name string) error {
	if name == "" {
		return errors.New("task name must not be empty")
	}
	for _, r := range name {
		switch {
		case 'a' <= r && r <= 'z':
		case 'A' <= r && r <= 'Z':
		case '0' <= r && r <= '9':
		case r == '-' || r == '_' || r == ':':
		default:
			return fmt.Errorf("task name %q contains disallowed character %q (allowed: letters, digits, '-', '_', ':')", name, r)
		}
	}
	if strings.HasPrefix(name, ":") || strings.HasSuffix(name, ":") || strings.Contains(name, "::") {
		return fmt.Errorf("task name %q must not start with, end with, or contain consecutive ':'", name)
	}
	return nil
}

// Parse reads and parses a task file from the given directory.
func Parse(dir string) (*Config, error) {
	path := findFile(dir)
	if path == "" {
		return nil, fmt.Errorf("no gogo.yaml found in %s", dir)
	}
	return parseFile(path, dir)
}

// parseFile reads and parses a YAML file at the given path. The dir argument
// becomes the Config's Dir, and is used to resolve relative paths for dotenv,
// nested includes and (for an included gogo.yaml) the task file's own dir.
func parseFile(path, dir string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var tf Config
	if err := yaml.UnmarshalWithOptions(data, &tf, yaml.Strict()); err != nil {
		return nil, fmt.Errorf("parsing %s:\n%s", path, yaml.FormatError(err, true, true))
	}

	tf.Dir = dir
	if tf.Tasks == nil {
		tf.Tasks = make(map[string]Task)
	}

	for name := range tf.Tasks {
		if err := validateTaskName(name); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}

	// Expand {{.VAR}} env-variable references on parsed string values only,
	// never on raw bytes — so an env value can't inject YAML structure.
	expandConfigEnvTemplates(&tf)

	// Extract comments from AST to use as task descriptions
	applyTaskComments(&tf, data)

	return &tf, nil
}

// findFile returns the path to the task file in dir, or empty if none exists.
func findFile(dir string) string {
	path := filepath.Join(dir, fileName)
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return ""
}

// FindRootDir walks up from dir to find the nearest directory containing a gogo.yaml.
func FindRootDir(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}

	for {
		if findFile(dir) != "" {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", errors.New("no gogo.yaml found")
}
