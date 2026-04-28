package taskfile

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
)

// LoadWithIncludes parses a task file and resolves all includes into a flat task map.
func LoadWithIncludes(dir string) (*Config, error) {
	root, err := Parse(dir)
	if err != nil {
		return nil, err
	}

	loader, err := newIncludeLoader(root)
	if err != nil {
		return nil, err
	}
	return loader.load()
}

// includeLoader holds shared state while recursively loading included task files.
type includeLoader struct {
	root         *Config
	rootDir      string
	seenDotenv   map[string]struct{} // deduplicates dotenv files across includes
	dotenvVars   map[string]string   // accumulated dotenv variables
	includeStack map[string]struct{} // dirs on the current recursion path (cycle detection)
}

func newIncludeLoader(root *Config) (*includeLoader, error) {
	seenDotenv := make(map[string]struct{})
	dotenvVars, err := loadDotenvFiles(root.Dir, root.Dotenv, seenDotenv)
	if err != nil {
		return nil, fmt.Errorf("loading dotenv: %w", err)
	}

	return &includeLoader{
		root:         root,
		rootDir:      root.Dir,
		seenDotenv:   seenDotenv,
		dotenvVars:   dotenvVars,
		includeStack: map[string]struct{}{root.Dir: {}},
	}, nil
}

func (l *includeLoader) load() (*Config, error) {
	l.root.Namespaces = make(map[string]string)

	for _, includeName := range l.root.Includes {
		if err := l.loadInclude(includeRequest{
			parentDir: l.rootDir,
			name:      includeName,
			namespace: includeName,
		}); err != nil {
			return nil, err
		}
	}

	l.root.DotenvVars = l.dotenvVars
	return l.root, nil
}

type includeRequest struct {
	parentDir string
	name      string
	namespace string
}

func (r includeRequest) parentFile() string {
	return findFile(r.parentDir)
}

func (r includeRequest) childDir() (string, error) {
	return filepath.Abs(filepath.Join(r.parentDir, r.name))
}

// loadInclude parses an included task file, loads nested includes, then merges it into the root.
func (l *includeLoader) loadInclude(req includeRequest) error {
	included, err := l.parseInclude(req)
	if err != nil {
		return err
	}
	defer l.leaveInclude(included.Dir)

	if err := l.loadIncludeDotenv(included); err != nil {
		return err
	}

	for _, nested := range nestedIncludes(included) {
		if err := l.loadInclude(nested); err != nil {
			return err
		}
	}

	l.mergeVars(included.Vars)
	l.mergeTasks(included)
	return nil
}

func (l *includeLoader) parseInclude(req includeRequest) (*includedConfig, error) {
	childDir, err := req.childDir()
	if err != nil {
		return nil, fmt.Errorf("resolving include %q from %s: %w", req.namespace, req.parentFile(), err)
	}
	if _, onPath := l.includeStack[childDir]; onPath {
		return nil, fmt.Errorf("cyclic include %q detected in %s", req.namespace, req.parentFile())
	}
	l.includeStack[childDir] = struct{}{}

	child, err := Parse(childDir)
	if err != nil {
		delete(l.includeStack, childDir)
		return nil, fmt.Errorf("loading include %q from %s: %w", req.namespace, req.parentFile(), err)
	}

	included := &includedConfig{
		Config:    child,
		Namespace: req.namespace,
		Parent:    req,
	}
	l.root.Namespaces[childDir] = req.namespace
	return included, nil
}

func (l *includeLoader) leaveInclude(dir string) {
	delete(l.includeStack, dir)
}

func (l *includeLoader) loadIncludeDotenv(included *includedConfig) error {
	childDotenv, err := loadDotenvFiles(included.Dir, included.Dotenv, l.seenDotenv)
	if err != nil {
		return fmt.Errorf("loading dotenv for include %q from %s: %w", included.Namespace, included.Parent.parentFile(), err)
	}
	for k, v := range childDotenv {
		if _, exists := l.dotenvVars[k]; !exists {
			l.dotenvVars[k] = v
		}
	}
	return nil
}

type includedConfig struct {
	*Config

	Namespace string
	Parent    includeRequest
}

func nestedIncludes(parent *includedConfig) []includeRequest {
	var requests []includeRequest
	for _, name := range parent.Includes {
		requests = append(requests, includeRequest{
			parentDir: parent.Dir,
			name:      name,
			namespace: parent.Namespace + ":" + name,
		})
	}
	return requests
}

// mergeVars merges child global vars into the root. Root vars win conflicts.
func (l *includeLoader) mergeVars(vars map[string]Var) {
	if len(vars) == 0 {
		return
	}
	if l.root.Vars == nil {
		l.root.Vars = make(map[string]Var)
	}
	for k, v := range vars {
		if _, exists := l.root.Vars[k]; !exists {
			l.root.Vars[k] = v
		}
	}
}

func (l *includeLoader) mergeTasks(included *includedConfig) {
	for _, name := range slices.Sorted(maps.Keys(included.Tasks)) {
		task := normalizedIncludedTask(included, name)
		l.root.Tasks[included.Namespace+":"+name] = task
	}
}

func normalizedIncludedTask(included *includedConfig, name string) Task {
	task := included.Tasks[name]
	makeTaskDirAbsolute(&task, included.Dir)
	namespaceLocalReferences(&task, included)
	return task
}

func makeTaskDirAbsolute(task *Task, fileDir string) {
	if !filepath.IsAbs(task.Dir) {
		task.Dir = filepath.Join(fileDir, task.Dir)
	}
}

func namespaceLocalReferences(task *Task, included *includedConfig) {
	for i, dep := range task.Deps {
		if hasTask(included.Tasks, dep.Task) {
			task.Deps[i].Task = included.Namespace + ":" + dep.Task
		}
	}
	for i, cmd := range task.Cmds {
		if cmd.Task != "" && hasTask(included.Tasks, cmd.Task) {
			task.Cmds[i].Task = included.Namespace + ":" + cmd.Task
		}
	}
}

func hasTask(tasks map[string]Task, name string) bool {
	_, ok := tasks[name]
	return ok
}
