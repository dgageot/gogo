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
	mu      sync.Mutex
}

// NewRunner creates a task runner for the given taskfile.
func NewRunner(tf *Taskfile, cwd string) *Runner {
	env := injectEnvVars(tf)

	// Build alias map for O(1) lookup
	aliases := make(map[string]string)
	for name, task := range tf.Tasks {
		for _, alias := range task.Aliases {
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

// dirPrefix returns the directory path with a trailing separator for prefix matching.
func dirPrefix(dir string) string {
	return dir + string(filepath.Separator)
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
		if !strings.HasPrefix(dirPrefix(r.cwd), dirPrefix(dir)) {
			continue
		}
		qualified := ns + ":" + name
		if _, ok := r.tf.Tasks[qualified]; ok {
			return qualified, true
		}
	}

	return name, false
}

// ensureSecrets resolves only the secrets requested by names, skipping any already loaded.
func (r *Runner) ensureSecrets(names []string) error {
	if len(names) == 0 || len(r.tf.Secrets) == 0 {
		return nil
	}

	r.mu.Lock()
	needed := make(map[string]string)
	for _, name := range names {
		if _, ok := r.tf.SecretVars[name]; ok {
			continue
		}
		if ref, ok := r.tf.Secrets[name]; ok {
			needed[name] = ref
		}
	}
	r.mu.Unlock()

	if len(needed) == 0 {
		return nil
	}

	secrets, err := loadSecrets(needed)
	if err != nil {
		return fmt.Errorf("loading secrets: %w", err)
	}

	r.mu.Lock()
	maps.Copy(r.tf.SecretVars, secrets)
	r.mu.Unlock()

	return nil
}

// Run executes the named task.
func (r *Runner) Run(name, cliArgs string) (err error) {
	resolved, ok := r.resolveTaskName(name)
	if !ok {
		return fmt.Errorf("task %q not found", name)
	}

	task := r.tf.Tasks[resolved]

	// Load only the secrets this task needs
	if err := r.ensureSecrets(task.Secrets); err != nil {
		return err
	}

	// Run dependencies concurrently
	if err := r.runDeps(task.Deps); err != nil {
		return err
	}

	// Resolve variables
	dir := r.taskDir(&task)
	vars := r.resolveVars(&task, dir)

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
	env := r.buildEnv(&task, vars)

	// Execute commands
	return r.runCmds(resolved, task.Cmds, vars, cliArgs, dir, env)
}

// runCmds executes a list of commands in sequence.
func (r *Runner) runCmds(taskName string, cmds []Cmd, vars map[string]string, cliArgs, dir string, env []string) error {
	for _, cmd := range cmds {
		if cmd.Task != "" {
			if err := r.Run(cmd.Task, cliArgs); err != nil {
				return err
			}
			continue
		}

		// Log the original command template to avoid leaking expanded secrets.
		logTask(colorGreen, taskName, cmd.Cmd)

		expanded := expandVars(cmd.Cmd, vars, cliArgs)
		if err := r.execCmd(taskName, expanded, dir, env); err != nil {
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
	if v.Sh != "" {
		cmd := exec.Command("/bin/sh", "-c", v.Sh)
		cmd.Dir = dir
		out, err := cmd.Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}

	return v.Value
}

// isUpToDate checks if the task sources are unchanged since the last run.
// Returns whether the task is up-to-date, the current checksum, and any error.
func (r *Runner) isUpToDate(task *Task, dir, taskName string) (bool, string, error) {
	if len(task.Sources) == 0 {
		return false, "", nil
	}

	checksum, err := sourcesChecksum(dir, task.Sources)
	if err != nil {
		return false, "", fmt.Errorf("computing sources checksum: %w", err)
	}

	return checksum == readStoredChecksum(r.tf.Dir, taskName), checksum, nil
}

// varLookup returns a function that resolves variable names from vars, then from the environment.
func varLookup(vars map[string]string) func(string) string {
	return func(key string) string {
		if val, ok := vars[key]; ok {
			return val
		}
		return os.Getenv(key)
	}
}

// buildEnv constructs the environment for a command execution.
func (r *Runner) buildEnv(task *Task, vars map[string]string) []string {
	env := slices.Clone(r.env)

	// Inject only the secrets requested by the task
	sortedSecrets := slices.Clone(task.Secrets)
	slices.Sort(sortedSecrets)
	for _, name := range sortedSecrets {
		if val, ok := r.tf.SecretVars[name]; ok {
			if _, exists := os.LookupEnv(name); !exists {
				env = append(env, envPair(name, val))
			}
		}
	}

	for _, k := range slices.Sorted(maps.Keys(vars)) {
		env = append(env, envPair(k, vars[k]))
	}

	lookup := varLookup(vars)
	for _, k := range slices.Sorted(maps.Keys(task.Env)) {
		env = append(env, envPair(k, os.Expand(task.Env[k], lookup)))
	}

	return env
}

// expandVars substitutes template and shell variables in a command string.
func expandVars(s string, vars map[string]string, cliArgs string) string {
	vars["CLI_ARGS"] = cliArgs

	lookup := func(key string) string {
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
func (r *Runner) execCmd(taskName, command, dir string, env []string) error {
	cmd := exec.Command("/bin/sh", "-c", command)
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
