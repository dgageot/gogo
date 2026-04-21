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

	return &tf, nil
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

	loader := &includeLoader{
		tf:           tf,
		seenDotenv:   seen,
		dotenvVars:   dotenvVars,
		includeStack: map[string]struct{}{dir: {}},
	}
	for _, namespace := range tf.Includes {
		if err := loader.load(dir, namespace, namespace); err != nil {
			return nil, err
		}
	}

	tf.DotenvVars = dotenvVars

	return tf, nil
}

// includeLoader holds shared state for recursively loading included Taskfiles.
type includeLoader struct {
	tf           *Taskfile
	seenDotenv   map[string]struct{} // deduplicates dotenv files across includes
	dotenvVars   map[string]string   // accumulated dotenv variables
	includeStack map[string]struct{} // dirs on the current recursion path (cycle detection)
}

// load parses a child Taskfile and merges it into the parent.
// dirName is the relative directory path for filesystem resolution.
// qualifiedNS is the fully qualified namespace prefix for task names (e.g. "cli:utils").
func (l *includeLoader) load(parentDir, dirName, qualifiedNS string) error {
	incDir := filepath.Join(parentDir, dirName)

	absIncDir, err := filepath.Abs(incDir)
	if err != nil {
		return fmt.Errorf("resolving include %q: %w", qualifiedNS, err)
	}
	if _, onPath := l.includeStack[absIncDir]; onPath {
		return fmt.Errorf("cyclic include detected for %q", absIncDir)
	}
	l.includeStack[absIncDir] = struct{}{}
	defer delete(l.includeStack, absIncDir)

	child, err := Parse(absIncDir)
	if err != nil {
		return fmt.Errorf("loading include %q: %w", qualifiedNS, err)
	}

	l.tf.Namespaces[absIncDir] = qualifiedNS

	// Load child dotenv files, deduplicating with parent
	childDotenv, err := loadDotenvFiles(absIncDir, child.Dotenv, l.seenDotenv)
	if err != nil {
		return fmt.Errorf("loading dotenv for include %q: %w", qualifiedNS, err)
	}
	for k, v := range childDotenv {
		if _, exists := l.dotenvVars[k]; !exists {
			l.dotenvVars[k] = v
		}
	}

	for _, childDir := range child.Includes {
		if err := l.load(absIncDir, childDir, qualifiedNS+":"+childDir); err != nil {
			return err
		}
	}

	// Merge child global vars into parent (parent wins on conflicts)
	if len(child.Vars) > 0 {
		if l.tf.Vars == nil {
			l.tf.Vars = make(map[string]Var)
		}
		for k, v := range child.Vars {
			if _, exists := l.tf.Vars[k]; !exists {
				l.tf.Vars[k] = v
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
				task.Deps[i].Task = qualifiedNS + ":" + dep.Task
			}
		}
		for i, cmd := range task.Cmds {
			if cmd.Task != "" {
				if _, ok := child.Tasks[cmd.Task]; ok {
					task.Cmds[i].Task = qualifiedNS + ":" + cmd.Task
				}
			}
		}

		l.tf.Tasks[qualifiedNS+":"+name] = task
	}

	return nil
}
