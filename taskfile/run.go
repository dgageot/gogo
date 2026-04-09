package taskfile

import (
	"cmp"
	"errors"
	"fmt"
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
}

// NewRunner creates a task runner for the given taskfile.
func NewRunner(tf *Taskfile, cwd string) *Runner {
	env := os.Environ()

	// Inject dotenv variables and keychain secrets (don't override existing env vars)
	for _, vars := range []map[string]string{tf.DotenvVars, tf.SecretVars} {
		for k, v := range vars {
			if _, exists := os.LookupEnv(k); !exists {
				env = append(env, k+"="+v)
			}
		}
	}

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
		if r.cwd == dir || strings.HasPrefix(r.cwd, dir+string(filepath.Separator)) {
			qualified := ns + ":" + name
			if _, ok := r.tf.Tasks[qualified]; ok {
				return qualified, true
			}
		}
	}

	return name, false
}

// Run executes the named task.
func (r *Runner) Run(name, cliArgs string) (err error) {
	resolved, ok := r.resolveTaskName(name)
	if !ok {
		return fmt.Errorf("task %q not found", name)
	}

	task := r.tf.Tasks[resolved]

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
	if len(task.Sources) > 0 {
		checksum, err := sourcesChecksum(dir, task.Sources)
		if err != nil {
			return fmt.Errorf("computing sources checksum: %w", err)
		}
		if checksum == readStoredChecksum(r.tf.Dir, resolved) {
			logTask(colorYellow, resolved, "up to date")
			return nil
		}
		defer func() {
			if err == nil {
				_ = writeChecksum(r.tf.Dir, resolved, checksum)
			}
		}()
	}

	// Build environment
	env := r.buildEnv(&task, vars)

	// Normalize single cmd into cmds list
	cmds := task.Cmds
	if !task.Cmd.isEmpty() {
		cmds = []Cmd{task.Cmd}
	}

	// Execute commands
	for _, cmd := range cmds {
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
// resolveVar evaluates a single variable, running a shell command if needed.
func (r *Runner) resolveVars(task *Task, taskDir string) map[string]string {
	resolved := make(map[string]string)

	// Built-in vars
	resolved["TASKFILE_DIR"] = taskDir

	// Global vars
	for k, v := range r.tf.Vars {
		resolved[k] = r.resolveVar(v, r.tf.Dir)
	}

	// Task vars override
	for k, v := range task.Vars {
		resolved[k] = r.resolveVar(v, taskDir)
	}

	return resolved
}

func (r *Runner) resolveVar(v Var, dir string) string {
	if v.Sh == "" {
		return v.Value
	}

	cmd := exec.Command("sh", "-c", v.Sh)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// buildEnv constructs the environment for a command execution.
func (r *Runner) buildEnv(task *Task, vars map[string]string) []string {
	env := slices.Clone(r.env)

	for k, v := range vars {
		env = append(env, k+"="+v)
	}
	for k, v := range task.Env {
		expanded := os.Expand(v, func(key string) string {
			if val, ok := vars[key]; ok {
				return val
			}
			return os.Getenv(key)
		})
		env = append(env, k+"="+expanded)
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
	var oldnew []string
	for k, v := range vars {
		oldnew = append(oldnew, "{{."+k+"}}", v)
	}
	oldnew = append(oldnew, "{{.CLI_ARGS}}", cliArgs)
	s = strings.NewReplacer(oldnew...).Replace(s)

	// Expand ${VAR} references; leave unknown ones for the shell
	return os.Expand(s, lookup)
}

func (r *Runner) runCmd(taskName, command, dir string, env []string) error {
	logTask(colorGreen, taskName, command)

	cmd := exec.Command("sh", "-c", command)
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
