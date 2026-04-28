package taskfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	yaml "github.com/goccy/go-yaml"
)

const fileName = "gogo.yaml"

// Parse reads and parses a task file from the given directory.
func Parse(dir string) (*Config, error) {
	path := findFile(dir)
	if path == "" {
		return nil, fmt.Errorf("no gogo.yaml found in %s", dir)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	data = expandTemplates(data)

	var tf Config
	if err := yaml.UnmarshalWithOptions(data, &tf, yaml.Strict()); err != nil {
		return nil, fmt.Errorf("parsing %s:\n%s", path, yaml.FormatError(err, true, true))
	}

	tf.Dir = dir
	if tf.Tasks == nil {
		tf.Tasks = make(map[string]Task)
	}

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
