package taskfile

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

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
	for _, name := range slices.Sorted(maps.Keys(tasks)) {
		task := tasks[name]
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

// FindRootDir walks up from dir to find the nearest directory containing a Taskfile.
func FindRootDir(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}

	for {
		if findTaskfile(dir) != "" {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", errors.New("no Taskfile found")
}

// LoadWithIncludes parses a Taskfile and resolves all includes into a flat task map.
func LoadWithIncludes(dir string) (*Taskfile, error) {
	tf, err := Parse(dir)
	if err != nil {
		return nil, err
	}

	tf.Namespaces = make(map[string]string)

	// Load dotenv files, deduplicating across includes
	seen := make(map[string]struct{})
	dotenvVars, err := loadDotenvFiles(dir, tf.Dotenv, seen)
	if err != nil {
		return nil, fmt.Errorf("loading dotenv: %w", err)
	}

	for _, namespace := range tf.Includes {
		if err := loadInclude(tf, dir, namespace, seen, dotenvVars, map[string]struct{}{dir: {}}); err != nil {
			return nil, err
		}
	}

	tf.DotenvVars = dotenvVars

	return tf, nil
}

// loadInclude parses a child Taskfile and merges it into the parent.
func loadInclude(tf *Taskfile, parentDir, namespace string, seen map[string]struct{}, dotenvVars map[string]string, includeStack map[string]struct{}) error {
	incDir := filepath.Join(parentDir, namespace)

	absIncDir, err := filepath.Abs(incDir)
	if err != nil {
		return fmt.Errorf("resolving include %q: %w", namespace, err)
	}
	if _, exists := includeStack[absIncDir]; exists {
		return fmt.Errorf("cyclic include detected for %q", absIncDir)
	}

	child, err := Parse(absIncDir)
	if err != nil {
		return fmt.Errorf("loading include %q: %w", namespace, err)
	}

	tf.Namespaces[absIncDir] = namespace

	// Load child dotenv files, deduplicating with parent
	childDotenv, err := loadDotenvFiles(absIncDir, child.Dotenv, seen)
	if err != nil {
		return fmt.Errorf("loading dotenv for include %q: %w", namespace, err)
	}
	for k, v := range childDotenv {
		if _, exists := dotenvVars[k]; !exists {
			dotenvVars[k] = v
		}
	}

	nextStack := make(map[string]struct{}, len(includeStack)+1)
	maps.Copy(nextStack, includeStack)
	nextStack[absIncDir] = struct{}{}
	for _, childNamespace := range child.Includes {
		if err := loadInclude(tf, absIncDir, childNamespace, seen, dotenvVars, nextStack); err != nil {
			return err
		}
	}

	// Merge child global vars into parent (parent wins on conflicts)
	if len(child.Vars) > 0 {
		if tf.Vars == nil {
			tf.Vars = make(map[string]Var)
		}
		for k, v := range child.Vars {
			if _, exists := tf.Vars[k]; !exists {
				tf.Vars[k] = v
			}
		}
	}

	for _, name := range slices.Sorted(maps.Keys(child.Tasks)) {
		task := child.Tasks[name]
		if !filepath.IsAbs(task.Dir) {
			task.Dir = filepath.Join(child.Dir, task.Dir)
		}

		// Namespace dep and cmd task references
		for i, dep := range task.Deps {
			if _, ok := child.Tasks[dep.Task]; ok {
				task.Deps[i].Task = namespace + ":" + dep.Task
			}
		}
		for i, cmd := range task.Cmds {
			if cmd.Task != "" {
				if _, ok := child.Tasks[cmd.Task]; ok {
					task.Cmds[i].Task = namespace + ":" + cmd.Task
				}
			}
		}

		tf.Tasks[namespace+":"+name] = task
	}

	return nil
}
