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

var templatePattern = regexp.MustCompile(`\{\{\s*\.([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

// expandTemplates replaces {{.VAR}} patterns with environment variable values.
func expandTemplates(data []byte) []byte {
	return templatePattern.ReplaceAllFunc(data, func(match []byte) []byte {
		name := string(templatePattern.FindSubmatch(match)[1])
		if val, ok := os.LookupEnv(name); ok {
			return []byte(val)
		}
		return match
	})
}

// Runner executes tasks from a loaded Taskfile.
type Runner struct {
	tf      *Taskfile
	cwd     string
	env     []string
	aliases map[string]string // alias -> task name
	DryRun  bool              // if true, print commands without executing them
	ran     sync.Map          // task name -> *runOnce
}

// runOnce tracks a single task execution for deduplication.
type runOnce struct {
	done chan struct{}
	err  error
}

// NewRunner creates a task runner for the given taskfile.
func NewRunner(tf *Taskfile, cwd string) *Runner {
	env := injectEnvVars(tf)

	// Build alias map for O(1) lookup
	aliases := make(map[string]string)
	for _, name := range slices.Sorted(maps.Keys(tf.Tasks)) {
		for _, alias := range tf.Tasks[name].Aliases {
			aliases[alias] = name
		}
	}

	return &Runner{
		tf:      tf,
		cwd:     cwd,
		env:     env,
		aliases: aliases,
	}
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
		if vars[name] == "" {
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

	// Try prefixing with namespace for cwd
	for _, dir := range slices.Sorted(maps.Keys(r.tf.Namespaces)) {
		ns := r.tf.Namespaces[dir]
		if !strings.HasPrefix(r.cwd+string(filepath.Separator), dir+string(filepath.Separator)) {
			continue
		}
		qualified := ns + ":" + name
		if _, ok := r.tf.Tasks[qualified]; ok {
			return qualified, true
		}
	}

	return name, false
}

// resolveTask finds a task by name and returns its resolved name and definition.
func (r *Runner) resolveTask(name string) (string, Task, error) {
	resolved, ok := r.resolveTaskName(name)
	if !ok {
		return "", Task{}, fmt.Errorf("task %q not found", name)
	}
	return resolved, r.tf.Tasks[resolved], nil
}

// Run executes the named task. Extra vars (from task call sites) override task-level vars.
func (r *Runner) Run(name, cliArgs string, extraVars ...map[string]Var) (err error) {
	resolved, _, err := r.resolveTask(name)
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
	vars := r.resolveVars(&task, dir)

	// Apply extra vars from call site
	for _, ev := range extraVars {
		for k, v := range ev {
			vars[k] = resolveVar(v, dir)
		}
	}

	// Validate required variables and environment
	if err := checkRequires(resolved, &task, vars); err != nil {
		return err
	}

	// Check sources for up-to-date
	upToDate, checksum, err := r.isUpToDate(&task, dir, resolved)
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
	if useOpRun {
		if _, err := exec.LookPath("op"); err != nil {
			return fmt.Errorf("task %q uses op:// secrets but the 1Password CLI (op) is not installed: %w\n\nInstall it from https://developer.1password.com/docs/cli/get-started/", resolved, err)
		}
	}

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
		if err := r.execCmd(taskName, expanded, dir, env, useOpRun); err != nil {
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
func (r *Runner) resolveVars(task *Task, taskDir string) map[string]string {
	resolved := map[string]string{
		"TASKFILE_DIR": taskDir,
	}

	// Global vars
	for k, v := range r.tf.Vars {
		resolved[k] = resolveVar(v, r.tf.Dir)
	}

	// Task vars override
	for k, v := range task.Vars {
		resolved[k] = resolveVar(v, taskDir)
	}

	return resolved
}

// resolveVar evaluates a single variable, running a shell command if needed.
func resolveVar(v Var, dir string) string {
	if v.Sh == "" {
		return v.Value
	}

	cmd := exec.Command("/bin/sh", "-c", v.Sh)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// isUpToDate checks if the task sources are unchanged since the last run.
// When generates is set, it checks that all outputs exist and are newer than all sources.
// Otherwise, it falls back to checksum-based comparison.
// Returns whether the task is up-to-date, the current checksum (empty when using generates), and any error.
func (r *Runner) isUpToDate(task *Task, dir, taskName string) (bool, string, error) {
	if len(task.Sources) == 0 {
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

// buildEnv constructs the environment for a command execution.
func (r *Runner) buildEnv(task *Task, dir string, vars map[string]string) ([]string, error) {
	env := slices.Clone(r.env)

	// Inject task-level dotenv vars (don't override existing env vars)
	if len(task.Dotenv) > 0 {
		taskDotenv, err := loadDotenvFiles(dir, task.Dotenv, make(map[string]struct{}))
		if err != nil {
			return nil, fmt.Errorf("loading task dotenv: %w", err)
		}
		for _, k := range slices.Sorted(maps.Keys(taskDotenv)) {
			if _, exists := os.LookupEnv(k); !exists {
				env = append(env, envPair(k, taskDotenv[k]))
			}
		}
	}

	for _, k := range slices.Sorted(maps.Keys(vars)) {
		env = append(env, envPair(k, vars[k]))
	}

	lookup := func(key string) string {
		if val, ok := vars[key]; ok {
			return val
		}
		return os.Getenv(key)
	}
	for _, k := range slices.Sorted(maps.Keys(task.Env)) {
		env = append(env, envPair(k, os.Expand(task.Env[k], lookup)))
	}

	return env, nil
}

// expandVars substitutes template and shell variables in a command string.
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

	// Replace {{.VAR}} templates
	s = templatePattern.ReplaceAllStringFunc(s, func(match string) string {
		name := templatePattern.FindStringSubmatch(match)[1]
		return lookup(name)
	})

	// Expand ${VAR} references; leave unknown ones for the shell
	return os.Expand(s, lookup)
}

// execCmd executes a shell command, wiring stdio.
// If useOpRun is true, the command is wrapped with "op run" to resolve op:// secrets.
func (r *Runner) execCmd(taskName, command, dir string, env []string, useOpRun bool) error {
	var cmd *exec.Cmd
	if useOpRun {
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
