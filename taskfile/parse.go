package taskfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	yaml "github.com/goccy/go-yaml"
)

// Parse reads and parses a Taskfile from the given directory.
func Parse(dir string) (*Taskfile, error) {
	path := findTaskfile(dir)
	if path == "" {
		return nil, fmt.Errorf("no Taskfile found in %s", dir)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	data = expandTemplates(data)

	var tf Taskfile
	if err := yaml.UnmarshalWithOptions(data, &tf, yaml.Strict()); err != nil {
		return nil, fmt.Errorf("parsing %s:\n%s", path, yaml.FormatError(err, true, true))
	}

	tf.Dir = dir
	if tf.Tasks == nil {
		tf.Tasks = make(map[string]Task)
	}

	// Extract comments from AST to use as task descriptions
	applyTaskComments(&tf, data)
	normalizeCmds(tf.Tasks)

	return &tf, nil
}

// normalizeCmds converts single cmd field to cmds list for each task.
func normalizeCmds(tasks map[string]Task) {
	for name, task := range tasks {
		if task.Cmd.isSet() {
			task.Cmds = []Cmd{task.Cmd}
			task.Cmd = Cmd{}
			tasks[name] = task
		}
	}
}

var taskfileNames = []string{"gogo.yaml", "Taskfile.yml", "Taskfile.yaml"}

// findTaskfile returns the path to a taskfile in dir, or empty if none exists.
func findTaskfile(dir string) string {
	for _, name := range taskfileNames {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// FindRootDir walks up from dir to find the topmost directory containing a Taskfile.
func FindRootDir(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}

	var found string
	for {
		if findTaskfile(dir) != "" {
			found = dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	if found == "" {
		return "", errors.New("no Taskfile found")
	}

	return found, nil
}

// LoadWithIncludes parses a Taskfile and resolves all includes into a flat task map.
func LoadWithIncludes(dir string) (*Taskfile, error) {
	tf, err := Parse(dir)
	if err != nil {
		return nil, err
	}

	tf.Namespaces = make(map[string]string)
	tf.SecretVars = make(map[string]string)

	// Load dotenv files, deduplicating across includes
	seen := make(map[string]struct{})
	dotenvVars, err := loadDotenvFiles(dir, tf.Dotenv, seen)
	if err != nil {
		return nil, fmt.Errorf("loading dotenv: %w", err)
	}

	for _, namespace := range tf.Includes {
		if err := loadInclude(tf, dir, namespace, seen, dotenvVars); err != nil {
			return nil, err
		}
	}

	tf.DotenvVars = dotenvVars

	return tf, nil
}

// loadInclude parses a child Taskfile and merges it into the parent.
func loadInclude(tf *Taskfile, parentDir, namespace string, seen map[string]struct{}, dotenvVars map[string]string) error {
	incDir := filepath.Join(parentDir, namespace)

	child, err := Parse(incDir)
	if err != nil {
		return fmt.Errorf("loading include %q: %w", namespace, err)
	}

	tf.Namespaces[incDir] = namespace

	// Load child dotenv files, deduplicating with parent
	childDotenv, err := loadDotenvFiles(incDir, child.Dotenv, seen)
	if err != nil {
		return fmt.Errorf("loading dotenv for include %q: %w", namespace, err)
	}
	for k, v := range childDotenv {
		if _, exists := dotenvVars[k]; !exists {
			dotenvVars[k] = v
		}
	}

	for name, task := range child.Tasks {
		if !filepath.IsAbs(task.Dir) {
			task.Dir = filepath.Join(child.Dir, task.Dir)
		}
		tf.Tasks[namespace+":"+name] = task
	}

	return nil
}
