package taskfile

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
)

var templatePattern = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile(`\{\{\s*\.([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)
})

// expandTemplates replaces {{.VAR}} patterns with environment variable values.
func expandTemplates(data []byte) []byte {
	re := templatePattern()
	return re.ReplaceAllFunc(data, func(match []byte) []byte {
		name := string(re.FindSubmatch(match)[1])
		if val, ok := os.LookupEnv(name); ok {
			return []byte(val)
		}
		return match
	})
}

// ExecFunc is the signature for command execution. It receives the task name,
// the expanded command string, working directory, environment, and whether
// to use "op run". Replacing this on a Runner allows tests to capture
// exactly which processes would be spawned, with which arguments and env,
// without forking any real process.
type ExecFunc func(taskName, command, dir string, env []string, useOpRun bool) error

// ResolveVarFunc resolves a variable value, optionally running a shell command.
// Replacing this on a Runner allows tests to avoid forking processes for
// variables that use "sh".
type ResolveVarFunc func(v Var, dir string) (string, error)

// Execution records a single command that was (or would be) executed.
type Execution struct {
	Task     string
	Command  string
	Dir      string
	Env      []string
	UseOpRun bool
}

// Runner executes tasks from a loaded Taskfile.
type Runner struct {
	tf             *Taskfile
	cwd            string
	BaseEnv        []string          // base process environment (defaults to os.Environ() + dotenv)
	aliases        map[string]string // alias -> task name
	DryRun         bool              // if true, print commands without executing them
	Force          bool              // if true, ignore sources and generates (always run)
	ExecFunc       ExecFunc          // replaceable command executor (defaults to real exec)
	ResolveVarFunc ResolveVarFunc    // replaceable variable resolver (defaults to shell exec)
	ran            sync.Map          // task name -> *runOnce
}

// runOnce tracks a single task execution for deduplication.
type runOnce struct {
	done chan struct{}
	err  error
}

// NewRunner creates a task runner for the given taskfile.
func NewRunner(tf *Taskfile, cwd string) (*Runner, error) {
	env := injectEnvVars(tf)

	// Build alias map for O(1) lookup
	aliases := make(map[string]string)
	for _, name := range slices.Sorted(maps.Keys(tf.Tasks)) {
		for _, alias := range tf.Tasks[name].Aliases {
			if existing, ok := aliases[alias]; ok {
				return nil, fmt.Errorf("alias %q is defined by both %q and %q", alias, existing, name)
			}
			aliases[alias] = name
		}
	}

	r := &Runner{
		tf:      tf,
		cwd:     cwd,
		BaseEnv: env,
		aliases: aliases,
	}
	r.ExecFunc = r.defaultExecFunc
	r.ResolveVarFunc = defaultResolveVar
	return r, nil
}

// envPair formats a key-value pair as an environment variable string.
func envPair(k, v string) string {
	return k + "=" + v
}

// injectEnvVars builds the process environment with dotenv vars injected.
func injectEnvVars(tf *Taskfile) []string {
	env := os.Environ()
	for _, k := range slices.Sorted(maps.Keys(tf.DotenvVars)) {
		if _, exists := os.LookupEnv(k); !exists {
			env = append(env, envPair(k, tf.DotenvVars[k]))
		}
	}
	return env
}

// matchesPlatform checks if the current OS/arch matches the platforms list.
// Each entry can be "os", "os/arch", or just "arch" (without a slash, matched against GOARCH if not a known OS).
// An empty list matches all platforms.
func matchesPlatform(platforms []string) bool {
	if len(platforms) == 0 {
		return true
	}
	for _, p := range platforms {
		goos, goarch, hasSlash := strings.Cut(p, "/")
		switch {
		case hasSlash:
			if goos == runtime.GOOS && goarch == runtime.GOARCH {
				return true
			}
		case goos == runtime.GOOS:
			return true
		case goos == runtime.GOARCH:
			return true
		}
	}
	return false
}

// checkRequires validates that all required vars and env are set.
func checkRequires(taskName string, task *Task, vars map[string]string) error {
	for _, name := range task.Requires.Vars {
		if _, ok := vars[name]; !ok {
			return fmt.Errorf("task %q requires variable %q to be set", taskName, name)
		}
	}
	for _, name := range task.Requires.Env {
		if _, ok := os.LookupEnv(name); !ok {
			return fmt.Errorf("task %q requires environment variable %q to be set", taskName, name)
		}
	}
	return nil
}

// checkPreconditions runs all precondition shell commands for a task.
// If any command fails, it returns an error with the precondition's message
// or a default message.
func (r *Runner) checkPreconditions(taskName string, task *Task, dir string) error {
	for _, pre := range task.Preconditions {
		cmd := exec.Command("/bin/sh", "-c", pre.Sh)
		cmd.Dir = dir
		if err := cmd.Run(); err != nil {
			if pre.Msg != "" {
				return fmt.Errorf("task %q: %s", taskName, pre.Msg)
			}
			return fmt.Errorf("task %q: precondition failed: %s", taskName, pre.Sh)
		}
	}
	return nil
}

// ResetRan clears the deduplication state, allowing tasks to run again.
// This is used by watch mode between iterations.
func (r *Runner) ResetRan() {
	r.ran = sync.Map{}
}

// resolveTaskName finds the actual task name, trying the exact name first,
// then aliases, then prefixing with the namespace matching the current working directory.
func (r *Runner) resolveTaskName(name string) (string, bool) {
	if _, ok := r.tf.Tasks[name]; ok {
		return name, true
	}

	// Try aliases
	if taskName, ok := r.aliases[name]; ok {
		return taskName, true
	}

	// Try prefixing with namespace for cwd — pick the most specific (longest) match
	var bestDir string
	var bestNS string
	for dir, ns := range r.tf.Namespaces {
		if !strings.HasPrefix(r.cwd+string(filepath.Separator), dir+string(filepath.Separator)) {
			continue
		}
		if len(dir) > len(bestDir) {
			bestDir = dir
			bestNS = ns
		}
	}
	if bestNS != "" {
		qualified := bestNS + ":" + name
		if _, ok := r.tf.Tasks[qualified]; ok {
			return qualified, true
		}
	}

	return name, false
}

// resolveTask finds a task by name and returns its resolved name.
func (r *Runner) resolveTask(name string) (string, error) {
	resolved, ok := r.resolveTaskName(name)
	if !ok {
		return "", fmt.Errorf("task %q not found", name)
	}
	return resolved, nil
}

// Run executes the named task. Extra vars (from task call sites) override task-level vars.
func (r *Runner) Run(name, cliArgs string, extraVars ...map[string]Var) (err error) {
	resolved, err := r.resolveTask(name)
	if err != nil {
		return err
	}

	// Deduplicate tasks called without extra vars (deps, plain references).
	// Tasks called with extra vars may produce different results, so skip dedup.
	hasExtraVars := len(extraVars) > 0 && len(extraVars[0]) > 0
	if !hasExtraVars {
		once := &runOnce{done: make(chan struct{})}
		if prev, loaded := r.ran.LoadOrStore(resolved, once); loaded {
			prev := prev.(*runOnce)
			<-prev.done
			return prev.err
		}
		defer func() {
			once.err = err
			close(once.done)
		}()
	}

	task := r.tf.Tasks[resolved]

	// Skip task if current platform doesn't match
	if !matchesPlatform(task.Platforms) {
		logTask(colorYellow, resolved, "skipped (platform mismatch)")
		return nil
	}

	// Run dependencies concurrently
	if err := r.runDeps(task.Deps); err != nil {
		return err
	}

	// Resolve variables
	dir := r.taskDir(&task)
	vars, err := r.resolveVars(&task, dir)
	if err != nil {
		return err
	}

	// Apply extra vars from call site
	for _, ev := range extraVars {
		for k, v := range ev {
			val, err := r.ResolveVarFunc(v, dir)
			if err != nil {
				return err
			}
			vars[k] = val
		}
	}

	// Validate required variables and environment
	if err := checkRequires(resolved, &task, vars); err != nil {
		return err
	}

	// Check preconditions
	if err := r.checkPreconditions(resolved, &task, dir); err != nil {
		return err
	}

	// Check sources for up-to-date
	upToDate, checksum, err := r.isUpToDate(&task, dir, resolved, r.Force)
	if err != nil {
		return err
	}
	if upToDate {
		logTask(colorYellow, resolved, "up to date")
		return nil
	}
	if checksum != "" {
		defer func() {
			if err == nil {
				_ = writeChecksum(r.tf.Dir, resolved, checksum)
			}
		}()
	}

	// Build environment
	env, err := r.buildEnv(&task, dir, vars)
	if err != nil {
		return err
	}

	// Execute commands
	useOpRun := hasOpSecrets(task.Env)

	return r.runCmds(resolved, task.Cmds, vars, cliArgs, dir, env, useOpRun)
}

// runCmds executes a list of commands in sequence.
func (r *Runner) runCmds(taskName string, cmds []Cmd, vars map[string]string, cliArgs, dir string, env []string, useOpRun bool) error {
	for _, cmd := range cmds {
		if cmd.Task != "" {
			if err := r.Run(cmd.Task, cliArgs, cmd.Vars); err != nil {
				return err
			}
			continue
		}

		// Log the original command template to avoid leaking expanded secrets.
		logTask(colorGreen, taskName, cmd.Cmd)

		if r.DryRun {
			continue
		}

		expanded := expandVars(cmd.Cmd, vars, cliArgs)
		if err := r.ExecFunc(taskName, expanded, dir, env, useOpRun); err != nil {
			return err
		}
	}
	return nil
}

// runDeps executes task dependencies concurrently.
func (r *Runner) runDeps(deps []Dep) error {
	if len(deps) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	errs := make([]error, len(deps))
	for i, dep := range deps {
		wg.Go(func() {
			errs[i] = r.Run(dep.Task, "")
		})
	}
	wg.Wait()

	return errors.Join(errs...)
}

// taskDir returns the working directory for a task.
func (r *Runner) taskDir(task *Task) string {
	dir := cmp.Or(task.Dir, r.tf.Dir)
	if filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Join(r.tf.Dir, dir)
}

// resolveVars computes the effective variables for a task.
func (r *Runner) resolveVars(task *Task, taskDir string) (map[string]string, error) {
	resolved := map[string]string{
		"TASKFILE_DIR": taskDir,
	}

	// Global vars (sorted for deterministic resolution)
	for _, k := range slices.Sorted(maps.Keys(r.tf.Vars)) {
		v, err := r.ResolveVarFunc(r.tf.Vars[k], r.tf.Dir)
		if err != nil {
			return nil, err
		}
		resolved[k] = v
	}

	// Task vars override (sorted for deterministic resolution)
	for _, k := range slices.Sorted(maps.Keys(task.Vars)) {
		v, err := r.ResolveVarFunc(task.Vars[k], taskDir)
		if err != nil {
			return nil, err
		}
		resolved[k] = v
	}

	return resolved, nil
}

// defaultResolveVar evaluates a single variable, running a shell command if needed.
func defaultResolveVar(v Var, dir string) (string, error) {
	if v.Sh == "" {
		return v.Value, nil
	}

	cmd := exec.Command("/bin/sh", "-c", v.Sh)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("resolving variable (sh: %s): %w", v.Sh, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// isUpToDate checks if the task sources are unchanged since the last run.
// When generates is set, it checks that all outputs exist and are newer than all sources.
// Otherwise, it falls back to checksum-based comparison.
// Returns whether the task is up-to-date, the current checksum (empty when using generates), and any error.
func (r *Runner) isUpToDate(task *Task, dir, taskName string, force bool) (bool, string, error) {
	if force || len(task.Sources) == 0 {
		return false, "", nil
	}

	// When generates is set, use timestamp-based comparison
	if len(task.Generates) > 0 {
		upToDate, err := outputsNewerThanSources(dir, task.Sources, task.Generates)
		return upToDate, "", err
	}

	checksum, err := sourcesChecksum(dir, task.Sources)
	if err != nil {
		return false, "", fmt.Errorf("computing sources checksum: %w", err)
	}

	// No files matched the patterns: always run.
	if checksum == "" {
		return false, "", nil
	}

	return checksum == readStoredChecksum(r.tf.Dir, taskName), checksum, nil
}

// hasOpSecrets reports whether any task env values contain op:// references.
func hasOpSecrets(env map[string]string) bool {
	for _, v := range env {
		if strings.HasPrefix(v, "op://") {
			return true
		}
	}
	return false
}

// envHasKey reports whether the env slice contains an entry for the given key.
func envHasKey(env []string, key string) bool {
	prefix := key + "="
	return slices.ContainsFunc(env, func(e string) bool {
		return strings.HasPrefix(e, prefix)
	})
}

// setEnv sets or replaces an environment variable in the env slice.
func setEnv(env []string, key, value string) []string {
	pair := envPair(key, value)
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = pair
			return env
		}
	}
	return append(env, pair)
}

// buildEnv constructs the environment for a command execution.
func (r *Runner) buildEnv(task *Task, dir string, vars map[string]string) ([]string, error) {
	env := slices.Clone(r.BaseEnv)

	// Inject task-level dotenv vars (don't override OS env or global dotenv vars)
	if len(task.Dotenv) > 0 {
		taskDotenv, err := loadDotenvFiles(dir, task.Dotenv, make(map[string]struct{}))
		if err != nil {
			return nil, fmt.Errorf("loading task dotenv: %w", err)
		}
		for _, k := range slices.Sorted(maps.Keys(taskDotenv)) {
			if !envHasKey(env, k) {
				env = append(env, envPair(k, taskDotenv[k]))
			}
		}
	}

	for _, k := range slices.Sorted(maps.Keys(vars)) {
		env = setEnv(env, k, vars[k])
	}

	resolvedEnv := make(map[string]string)
	var resolve func(string) string
	resolve = func(key string) string {
		if val, ok := resolvedEnv[key]; ok {
			return val
		}
		if raw, ok := task.Env[key]; ok {
			val := os.Expand(raw, func(k string) string {
				if k == key {
					return "" // prevent infinite recursion
				}
				return resolve(k)
			})
			resolvedEnv[key] = val
			return val
		}
		if val, ok := vars[key]; ok {
			return val
		}
		return os.Getenv(key)
	}
	for _, k := range slices.Sorted(maps.Keys(task.Env)) {
		env = setEnv(env, k, resolve(k))
	}

	return env, nil
}

// expandVars substitutes template and shell variables in a command string.
// {{.VAR}} and ${VAR} are both resolved from task variables, CLI_ARGS, and environment.
func expandVars(s string, vars map[string]string, cliArgs string) string {
	lookup := func(key string) string {
		if key == "CLI_ARGS" {
			return cliArgs
		}
		if val, ok := vars[key]; ok {
			return val
		}
		if val, ok := os.LookupEnv(key); ok {
			return val
		}
		return "${" + key + "}"
	}

	// Expand ${VAR} first. This won't touch {{.VAR}} templates since they
	// don't start with $. Unknown variables are left as ${KEY} for the shell.
	s = os.Expand(s, lookup)

	// Then replace {{.VAR}} templates. Since os.Expand already ran,
	// the expanded values won't be re-processed, preventing double expansion.
	re := templatePattern()
	return re.ReplaceAllStringFunc(s, func(match string) string {
		return lookup(re.FindStringSubmatch(match)[1])
	})
}

// defaultExecFunc executes a shell command, wiring stdio.
// If useOpRun is true, the command is wrapped with "op run" to resolve op:// secrets.
func (r *Runner) defaultExecFunc(taskName, command, dir string, env []string, useOpRun bool) error {
	var cmd *exec.Cmd
	if useOpRun {
		if _, err := exec.LookPath("op"); err != nil {
			return fmt.Errorf("task %q uses op:// secrets but the 1Password CLI (op) is not installed: %w\n\nInstall it from https://developer.1password.com/docs/cli/get-started/", taskName, err)
		}
		cmd = exec.Command("op", "run", "--", "/bin/sh", "-c", command)
	} else {
		cmd = exec.Command("/bin/sh", "-c", command)
	}
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("task %q: %w", taskName, err)
	}
	return nil
}
