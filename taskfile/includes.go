package taskfile

import (
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
)

// validateIncludeName ensures an `includes:` entry resolves to a direct
// subdirectory of the task file that declares it. Absolute paths,
// parent-directory escapes ('..'), and path separators are rejected so an
// include can never reach outside its parent's tree.
func validateIncludeName(name string) error {
	if name == "" {
		return errors.New("include name must not be empty")
	}
	if filepath.IsAbs(name) {
		return fmt.Errorf("include %q must be a subdirectory (absolute paths are not allowed)", name)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("include %q must be a subdirectory (path separators are not allowed)", name)
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("include %q must be a subdirectory ('..' is not allowed)", name)
	}
	return nil
}

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

// includeLoader holds shared state while recursively loading task files
// pulled in via `includes:` (namespaced) and `flatten:` (no namespace).
type includeLoader struct {
	root       *Config
	rootDir    string
	seenDotenv map[string]struct{} // deduplicates dotenv files across loaded files
	dotenvVars map[string]string   // accumulated dotenv variables
	loadStack  map[string]struct{} // absolute file paths currently being loaded (cycle detection)
}

func newIncludeLoader(root *Config) (*includeLoader, error) {
	seenDotenv := make(map[string]struct{})
	dotenvVars, err := loadDotenvFiles(root.Dir, root.Dotenv, seenDotenv)
	if err != nil {
		return nil, fmt.Errorf("loading dotenv: %w", err)
	}

	return &includeLoader{
		root:       root,
		rootDir:    root.Dir,
		seenDotenv: seenDotenv,
		dotenvVars: dotenvVars,
		loadStack:  map[string]struct{}{filepath.Join(root.Dir, fileName): {}},
	}, nil
}

func (l *includeLoader) load() (*Config, error) {
	l.root.Namespaces = make(map[string]string)
	l.root.NamespaceDirs = make(map[string]string)
	l.root.NamespaceVars = make(map[string]map[string]Var)

	// Process flatten files first (their tasks merge into the root namespace).
	// Order vs. includes doesn't matter for correctness because both honor
	// "first defined wins"; the root's own tasks were already loaded at Parse.
	for _, p := range l.root.Flatten {
		if err := l.loadFlatten(flattenRequest{
			parentDir:   l.rootDir,
			parentFile:  filepath.Join(l.rootDir, fileName),
			path:        p,
			namespace:   "",
			ancestorDir: l.rootDir,
		}); err != nil {
			return nil, err
		}
	}

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
	if err := validateDefaultTask(l.root); err != nil {
		return nil, err
	}
	if err := validateSecrets(l.root); err != nil {
		return nil, err
	}
	return l.root, nil
}

// validateDefaultTask ensures that a top-level `default:` field references
// a real task. Catching this at load time gives the user a clear error
// instead of the generic "task not found" they'd otherwise see at run time.
func validateDefaultTask(c *Config) error {
	if c.Default == "" {
		return nil
	}
	if _, ok := c.Tasks[c.Default]; ok {
		return nil
	}
	return fmt.Errorf("top-level default: %q does not reference any defined task", c.Default)
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

// loadInclude parses an included task file, loads nested includes and flatten
// files, then merges it into the root.
func (l *includeLoader) loadInclude(req includeRequest) error {
	included, err := l.parseInclude(req)
	if err != nil {
		return err
	}
	defer l.leaveLoad(filepath.Join(included.Dir, fileName))

	if err := l.loadIncludeDotenv(included); err != nil {
		return err
	}

	// Process flatten files declared inside this include — their tasks merge
	// at this include's namespace.
	for _, p := range included.Flatten {
		if err := l.loadFlatten(flattenRequest{
			parentDir:   included.Dir,
			parentFile:  filepath.Join(included.Dir, fileName),
			path:        p,
			namespace:   included.Namespace,
			ancestorDir: included.Dir,
		}); err != nil {
			return err
		}
	}

	for _, name := range included.Includes {
		if err := l.loadInclude(includeRequest{
			parentDir: included.Dir,
			name:      name,
			namespace: included.Namespace + ":" + name,
		}); err != nil {
			return err
		}
	}

	l.mergeVars(included.Namespace, included.Dir, included.Vars)
	l.mergeSourcePresets(included.Sources)
	l.mergeSecrets(included.Secrets)
	return l.mergeTasks(included)
}

func (l *includeLoader) parseInclude(req includeRequest) (*includedConfig, error) {
	if err := validateIncludeName(req.name); err != nil {
		return nil, fmt.Errorf("%s: %w", req.parentFile(), err)
	}
	childDir, err := req.childDir()
	if err != nil {
		return nil, fmt.Errorf("resolving include %q from %s: %w", req.namespace, req.parentFile(), err)
	}
	expectedFile := filepath.Join(childDir, fileName)
	if _, onPath := l.loadStack[expectedFile]; onPath {
		return nil, fmt.Errorf("cyclic include %q detected in %s", req.namespace, req.parentFile())
	}
	l.loadStack[expectedFile] = struct{}{}

	child, err := Parse(childDir)
	if err != nil {
		delete(l.loadStack, expectedFile)
		return nil, fmt.Errorf("loading include %q from %s: %w", req.namespace, req.parentFile(), err)
	}

	included := &includedConfig{
		Config:    child,
		Namespace: req.namespace,
		Parent:    req,
	}
	l.root.Namespaces[childDir] = req.namespace
	l.root.NamespaceDirs[req.namespace] = childDir
	return included, nil
}

func (l *includeLoader) leaveLoad(filePath string) {
	delete(l.loadStack, filePath)
}

func (l *includeLoader) loadIncludeDotenv(included *includedConfig) error {
	childDotenv, err := loadDotenvFiles(included.Dir, included.Dotenv, l.seenDotenv)
	if err != nil {
		return fmt.Errorf("loading dotenv for include %q from %s: %w", included.Namespace, included.Parent.parentFile(), err)
	}
	l.dotenvVars = mergeFirstWins(l.dotenvVars, childDotenv)
	return nil
}

type includedConfig struct {
	*Config

	Namespace string
	Parent    includeRequest
}

// flattenRequest describes a single `flatten:` entry pulled from a parent
// task file. Tasks loaded via flatten merge into the parent's namespace
// (or the root namespace when invoked from the root) without a prefix.
type flattenRequest struct {
	parentDir   string // dir of the file that declared this flatten (path resolution base)
	parentFile  string // absolute path of that file (for error messages and cycle detection)
	path        string // the path string written in YAML
	namespace   string // namespace at which to merge tasks ("" means the root)
	ancestorDir string // dir of the namespaced ancestor (root, or include dir) for resolving task.Dir
}

// loadFlatten parses a flatten YAML file, resolves its nested flatten/includes,
// then merges its tasks into l.root at the given namespace.
func (l *includeLoader) loadFlatten(req flattenRequest) error {
	absPath, err := filepath.Abs(resolvePath(req.parentDir, req.path))
	if err != nil {
		return fmt.Errorf("resolving flatten %q from %s: %w", req.path, req.parentFile, err)
	}
	if _, onPath := l.loadStack[absPath]; onPath {
		return fmt.Errorf("cyclic flatten %q detected in %s", req.path, req.parentFile)
	}
	l.loadStack[absPath] = struct{}{}
	defer l.leaveLoad(absPath)

	flattenedDir := filepath.Dir(absPath)
	flattened, err := parseFile(absPath, flattenedDir)
	if err != nil {
		return fmt.Errorf("loading flatten %q from %s: %w", req.path, req.parentFile, err)
	}

	// Load this file's dotenv (paths resolve relative to the file's own dir).
	childDotenv, err := loadDotenvFiles(flattened.Dir, flattened.Dotenv, l.seenDotenv)
	if err != nil {
		return fmt.Errorf("loading dotenv for flatten %q from %s: %w", req.path, req.parentFile, err)
	}
	l.dotenvVars = mergeFirstWins(l.dotenvVars, childDotenv)

	// Recurse: nested flatten files keep the same namespace and ancestor.
	for _, p := range flattened.Flatten {
		if err := l.loadFlatten(flattenRequest{
			parentDir:   flattened.Dir,
			parentFile:  absPath,
			path:        p,
			namespace:   req.namespace,
			ancestorDir: req.ancestorDir,
		}); err != nil {
			return err
		}
	}

	// Recurse: nested includes get sub-namespaces under our current one.
	for _, name := range flattened.Includes {
		if err := l.loadInclude(includeRequest{
			parentDir: flattened.Dir,
			name:      name,
			namespace: namespaceJoin(req.namespace, name),
		}); err != nil {
			return err
		}
	}

	l.mergeVars(req.namespace, req.ancestorDir, flattened.Vars)
	l.mergeSourcePresets(flattened.Sources)
	return l.mergeFlattenedTasks(flattened, req.namespace, req.ancestorDir)
}

func namespaceJoin(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + ":" + name
}

// mergeFirstWins copies entries from src into dst, keeping existing dst
// entries on a key collision ("first defined wins"). It allocates dst when
// nil and returns the (possibly new) map.
func mergeFirstWins[V any](dst, src map[string]V) map[string]V {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = make(map[string]V, len(src))
	}
	for k, v := range src {
		if _, exists := dst[k]; !exists {
			dst[k] = v
		}
	}
	return dst
}

// mergeVars merges vars declared by an included or flattened file into the
// scope that owns them. Vars from the root or root-level flatten files
// (namespace "") land in Config.Vars and are visible everywhere; vars from
// an include land in Config.NamespaceVars[namespace] and are visible only
// to tasks that live at or below that namespace — so two sibling includes
// can each declare their own LDFLAGS/IMAGE_NAME without leaking values
// into each other. "First defined wins" still holds within each scope,
// matching the prior root-merge precedence.
func (l *includeLoader) mergeVars(namespace, dir string, vars map[string]Var) {
	if len(vars) == 0 {
		return
	}
	if namespace == "" {
		l.root.Vars = mergeFirstWins(l.root.Vars, vars)
		return
	}
	if _, ok := l.root.NamespaceDirs[namespace]; !ok {
		l.root.NamespaceDirs[namespace] = dir
	}
	l.root.NamespaceVars[namespace] = mergeFirstWins(l.root.NamespaceVars[namespace], vars)
}

// mergeSourcePresets merges child source presets into the root. Root presets
// win conflicts, mirroring the precedence used by mergeVars.
func (l *includeLoader) mergeSourcePresets(presets map[string]StringList) {
	l.root.Sources = mergeFirstWins(l.root.Sources, presets)
}

// mergeSecrets merges child secret declarations into the root. Root entries
// win conflicts, mirroring the precedence used by mergeVars.
func (l *includeLoader) mergeSecrets(secrets map[string]string) {
	l.root.Secrets = mergeFirstWins(l.root.Secrets, secrets)
}

func (l *includeLoader) mergeTasks(included *includedConfig) error {
	for _, name := range slices.Sorted(maps.Keys(included.Tasks)) {
		task := normalizedIncludedTask(included, name)
		finalName := included.Namespace + ":" + name
		if err := validateTaskName(finalName); err != nil {
			return err
		}
		l.root.Tasks[finalName] = task
	}
	return nil
}

// mergeFlattenedTasks merges tasks from a flattened file into l.root at the
// given namespace. With an empty namespace, tasks land in the root unchanged
// (this is the agentic-platform pattern for splitting one file across many).
// First defined wins, so a parent file can override a flattened task by
// declaring it locally with the same name.
func (l *includeLoader) mergeFlattenedTasks(flattened *Config, namespace, ancestorDir string) error {
	for _, name := range slices.Sorted(maps.Keys(flattened.Tasks)) {
		finalName := namespaceJoin(namespace, name)
		if err := validateTaskName(finalName); err != nil {
			return err
		}
		if _, exists := l.root.Tasks[finalName]; exists {
			continue // first wins (root or earlier flatten file beats this one)
		}
		task := flattened.Tasks[name]
		makeTaskDirAbsolute(&task, ancestorDir)
		if namespace != "" {
			ic := &includedConfig{Config: flattened, Namespace: namespace}
			namespaceLocalReferences(&task, ic)
		}
		l.root.Tasks[finalName] = task
	}
	return nil
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
	// Aliases are namespaced like the task name so two included files can
	// each declare the same bare alias (e.g. `up`) without colliding in the
	// runner's global alias map.
	if len(task.Aliases) > 0 {
		aliases := make(StringList, len(task.Aliases))
		for i, alias := range task.Aliases {
			aliases[i] = included.Namespace + ":" + alias
		}
		task.Aliases = aliases
	}
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
