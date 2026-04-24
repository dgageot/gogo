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
	runs           sync.Map          // resolved task name -> *taskRun
}

// taskRun memoizes a single task execution. The first caller runs the body;
// concurrent and later callers observe the same result.
type taskRun struct {
	once sync.Once
	err  error
}

// do runs fn exactly once, returning its memoized result to every caller.
func (t *taskRun) do(fn func() error) error {
	t.once.Do(func() { t.err = fn() })
	return t.err
}

// NewRunner creates a task runner for the given taskfile.
func NewRunner(tf *Taskfile, cwd string) (*Runner, error) {
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
		BaseEnv: baseEnvWithDotenv(tf.DotenvVars),
		aliases: aliases,
	}
	r.ExecFunc = r.defaultExecFunc
	r.ResolveVarFunc = defaultResolveVar
	return r, nil
}

// matchesPlatform reports whether the current OS/arch matches any entry in
// platforms. Each entry is either "os/arch", a bare OS name, or a bare arch.
// An empty list matches every platform.
func matchesPlatform(platforms []string) bool {
	for _, p := range platforms {
		goos, goarch, hasSlash := strings.Cut(p, "/")
		if hasSlash {
			if goos == runtime.GOOS && goarch == runtime.GOARCH {
				return true
			}
			continue
		}
		if goos == runtime.GOOS || goos == runtime.GOARCH {
			return true
		}
	}
	return len(platforms) == 0
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
// Preconditions see the same environment as the task's commands.
// If any command fails, it returns an error with the precondition's message
// or a default message.
func (r *Runner) checkPreconditions(taskName string, task *Task, dir string, env []string) error {
	for _, pre := range task.Preconditions {
		cmd := exec.Command("/bin/sh", "-c", pre.Sh)
		cmd.Dir = dir
		cmd.Env = env
		if err := cmd.Run(); err != nil {
			if pre.Msg != "" {
				return fmt.Errorf("task %q: %s", taskName, pre.Msg)
			}
			return fmt.Errorf("task %q: precondition failed: %s", taskName, pre.Sh)
		}
	}
	return nil
}

// ResetRan clears the memoized task results, allowing tasks to run again.
// This is used by watch mode between iterations.
func (r *Runner) ResetRan() {
	r.runs = sync.Map{}
}

// resolveTaskName finds the actual task name, trying the exact name first,
// then aliases, then prefixing with the namespace matching the current working directory.
func (r *Runner) resolveTaskName(name string) (string, bool) {
	if _, ok := r.tf.Tasks[name]; ok {
		return name, true
	}

	if taskName, ok := r.aliases[name]; ok {
		return taskName, true
	}

	if ns, ok := r.cwdNamespace(); ok {
		if _, ok := r.tf.Tasks[ns+":"+name]; ok {
			return ns + ":" + name, true
		}
	}

	// Try stripping a self-prefix that matches the taskfile root's basename.
	// This lets "proxy:deploy-prod" work when running from a sub-Taskfile at
	// proxy/ — the same name also works when the parent Taskfile is the root.
	if prefix, suffix, ok := strings.Cut(name, ":"); ok && prefix == filepath.Base(r.tf.Dir) {
		if resolved, ok := r.resolveTaskName(suffix); ok {
			return resolved, true
		}
	}

	return name, false
}

// cwdNamespace returns the most specific namespace whose directory contains
// the runner's current working directory. Used to let users invoke tasks by
// their short name when cwd sits under an included Taskfile.
func (r *Runner) cwdNamespace() (string, bool) {
	var bestDir, bestNS string
	for dir, ns := range r.tf.Namespaces {
		if !strings.HasPrefix(r.cwd+string(filepath.Separator), dir+string(filepath.Separator)) {
			continue
		}
		if len(dir) > len(bestDir) {
			bestDir = dir
			bestNS = ns
		}
	}
	return bestNS, bestNS != ""
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
func (r *Runner) Run(name, cliArgs string, extraVars ...map[string]Var) error {
	resolved, err := r.resolveTask(name)
	if err != nil {
		return err
	}

	// Each call site with extra vars is a distinct execution — bypass memoization.
	if hasExtraVars(extraVars) {
		return r.run(resolved, cliArgs, extraVars)
	}

	entry, _ := r.runs.LoadOrStore(resolved, &taskRun{})
	return entry.(*taskRun).do(func() error {
		return r.run(resolved, cliArgs, nil)
	})
}

// hasExtraVars reports whether the variadic extraVars carries any overrides.
func hasExtraVars(extraVars []map[string]Var) bool {
	return len(extraVars) > 0 && len(extraVars[0]) > 0
}

// run executes a task's body. Deduplication is handled by Run; this method
// always runs the task, so recursive calls from runCmds must go through Run.
func (r *Runner) run(resolved, cliArgs string, extraVars []map[string]Var) error {
	task := r.tf.Tasks[resolved]

	if !matchesPlatform(task.Platforms) {
		logTask(colorYellow, resolved, "skipped (platform mismatch)")
		return nil
	}

	if err := r.runDeps(task.Deps); err != nil {
		return err
	}

	dir := r.taskDir(&task)

	vars, err := r.resolveAllVars(&task, dir, extraVars)
	if err != nil {
		return err
	}

	if err := checkRequires(resolved, &task, vars); err != nil {
		return err
	}

	env, err := r.buildEnv(&task, dir, vars)
	if err != nil {
		return err
	}

	if err := r.checkPreconditions(resolved, &task, dir, env); err != nil {
		return err
	}

	upToDate, checksum, err := r.isUpToDate(&task, dir, resolved, r.Force)
	if err != nil {
		return err
	}
	if upToDate {
		logTask(colorYellow, resolved, "up to date")
		return nil
	}

	if err := r.runCmds(resolved, task.Cmds, vars, cliArgs, dir, env, hasOpSecrets(env)); err != nil {
		return err
	}

	if checksum != "" {
		_ = writeChecksum(r.tf.Dir, resolved, checksum) // best-effort
	}
	return nil
}

// resolveAllVars computes the effective variables for a task, including extra vars from call sites.
func (r *Runner) resolveAllVars(task *Task, dir string, extraVars []map[string]Var) (map[string]string, error) {
	vars, err := r.resolveVars(task, dir)
	if err != nil {
		return nil, err
	}

	for _, ev := range extraVars {
		if err := r.addVars(vars, ev, dir); err != nil {
			return nil, err
		}
	}

	return vars, nil
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
	if err := r.addVars(resolved, r.tf.Vars, r.tf.Dir); err != nil {
		return nil, err
	}
	if err := r.addVars(resolved, task.Vars, taskDir); err != nil {
		return nil, err
	}
	return resolved, nil
}

// addVars resolves each Var in src (sorted for determinism) and writes it into dst.
func (r *Runner) addVars(dst map[string]string, src map[string]Var, dir string) error {
	for _, k := range slices.Sorted(maps.Keys(src)) {
		v, err := r.ResolveVarFunc(src[k], dir)
		if err != nil {
			return err
		}
		dst[k] = v
	}
	return nil
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

// expandVars substitutes template and shell variables in a command string.
// {{.VAR}} and ${VAR} are both resolved from task variables, CLI_ARGS, and environment.
// Unknown ${VAR} references are left for the shell to expand. Unknown
// {{.VAR}} templates are left verbatim (matching expandTemplates at parse time).
func expandVars(s string, vars map[string]string, cliArgs string) string {
	lookup := func(key string) (string, bool) {
		if key == "CLI_ARGS" {
			return cliArgs, true
		}
		if val, ok := vars[key]; ok {
			return val, true
		}
		return os.LookupEnv(key)
	}

	// Expand ${VAR} first. This won't touch {{.VAR}} templates since they
	// don't start with $. Unknown variables are left as ${KEY} for the shell.
	s = os.Expand(s, func(key string) string {
		if val, ok := lookup(key); ok {
			return val
		}
		return "${" + key + "}"
	})

	// Then replace {{.VAR}} templates. Unknown templates are left as-is so
	// run-time behavior matches expandTemplates at parse time.
	return templatePattern.ReplaceAllStringFunc(s, func(match string) string {
		key := templatePattern.FindStringSubmatch(match)[1]
		if val, ok := lookup(key); ok {
			return val
		}
		return match
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
