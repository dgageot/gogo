package taskfile

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

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

// injectEnvVars builds the process environment with dotenv vars injected.
func injectEnvVars(tf *Taskfile) []string {
	env := os.Environ()
	for _, k := range slices.Sorted(maps.Keys(tf.DotenvVars)) {
		if _, exists := os.LookupEnv(k); !exists {
			env = append(env, k+"="+tf.DotenvVars[k])
		}
	}
	return env
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
	for dir, ns := range r.tf.Namespaces {
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

// ensureSecrets resolves only the secrets requested by names, skipping any already loaded.
func (r *Runner) ensureSecrets(names []string) error {
	if len(names) == 0 || len(r.tf.Secrets) == 0 {
		return nil
	}

	// Collect entries that still need to be resolved.
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
	if len(task.Deps) > 0 {
		var wg sync.WaitGroup
		errs := make([]error, len(task.Deps))
		for i, dep := range task.Deps {
			wg.Go(func() {
				errs[i] = r.Run(dep.Task, "")
			})
		}
		wg.Wait()
		if err := errors.Join(errs...); err != nil {
			return err
		}
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
	for _, cmd := range task.Cmds {
		if cmd.Task != "" {
			if err := r.Run(cmd.Task, cliArgs); err != nil {
				return err
			}
			continue
		}
		if err := r.runCmd(resolved, r.expandVars(cmd.Cmd, vars, cliArgs), dir, env); err != nil {
			return err
		}
	}

	return nil
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

// buildEnv constructs the environment for a command execution.
func (r *Runner) buildEnv(task *Task, vars map[string]string) []string {
	env := slices.Clone(r.env)

	// Inject only the secrets requested by the task
	for _, name := range slices.Sorted(slices.Values(task.Secrets)) {
		if val, ok := r.tf.SecretVars[name]; ok {
			if _, exists := os.LookupEnv(name); !exists {
				env = append(env, name+"="+val)
			}
		}
	}

	for _, k := range slices.Sorted(maps.Keys(vars)) {
		env = append(env, k+"="+vars[k])
	}

	lookup := func(key string) string {
		if val, ok := vars[key]; ok {
			return val
		}
		return os.Getenv(key)
	}
	for _, k := range slices.Sorted(maps.Keys(task.Env)) {
		env = append(env, k+"="+os.Expand(task.Env[k], lookup))
	}

	return env
}

// expandVars substitutes template and shell variables in a command string.
func (r *Runner) expandVars(s string, vars map[string]string, cliArgs string) string {
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

// runCmd executes a shell command, logging it and wiring stdio.
func (r *Runner) runCmd(taskName, command, dir string, env []string) error {
	logTask(colorGreen, taskName, command)

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
