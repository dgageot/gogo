package taskfile

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Runner executes tasks from a loaded Taskfile.
type Runner struct {
	tf  *Taskfile
	cwd string
	env []string
}

// NewRunner creates a task runner for the given taskfile.
func NewRunner(tf *Taskfile, cwd string) *Runner {
	env := os.Environ()

	// Inject dotenv variables (don't override existing env vars)
	for k, v := range tf.DotenvVars {
		if _, exists := os.LookupEnv(k); !exists {
			env = append(env, k+"="+v)
		}
	}

	// Inject keychain secrets (don't override existing env vars)
	for k, v := range tf.SecretVars {
		if _, exists := os.LookupEnv(k); !exists {
			env = append(env, k+"="+v)
		}
	}

	return &Runner{
		tf:  tf,
		cwd: cwd,
		env: env,
	}
}

// resolveTaskName finds the actual task name, trying the exact name first,
// then aliases, then prefixing with the namespace matching the current working directory.
func (r *Runner) resolveTaskName(name string) (string, bool) {
	if _, ok := r.tf.Tasks[name]; ok {
		return name, true
	}

	// Try aliases
	for taskName, task := range r.tf.Tasks {
		for _, alias := range task.Aliases {
			if alias == name {
				return taskName, true
			}
		}
	}

	// Try prefixing with namespace for cwd
	for dir, ns := range r.tf.Namespaces {
		if r.cwd == dir || strings.HasPrefix(r.cwd, dir+string(os.PathSeparator)) {
			qualified := ns + ":" + name
			if _, ok := r.tf.Tasks[qualified]; ok {
				return qualified, true
			}
		}
	}

	return name, false
}

// Run executes the named task.
func (r *Runner) Run(name string, cliArgs string) (err error) {
	resolved, ok := r.resolveTaskName(name)
	if !ok {
		return fmt.Errorf("task %q not found", name)
	}

	task := r.tf.Tasks[resolved]

	// Run dependencies concurrently
	if len(task.Deps) > 0 {
		errs := make(chan error, len(task.Deps))
		for _, dep := range task.Deps {
			go func() {
				errs <- r.Run(dep.Task, "")
			}()
		}
		for range task.Deps {
			if err := <-errs; err != nil {
				return err
			}
		}
	}

	// Resolve variables
	vars := r.resolveVars(task)

	// Determine working directory
	dir := r.taskDir(task)

	// Check sources for up-to-date
	if len(task.Sources) > 0 {
		checksum, err := sourcesChecksum(dir, task.Sources)
		if err != nil {
			return fmt.Errorf("computing sources checksum: %w", err)
		}
		if checksum == readStoredChecksum(r.tf.Dir, resolved) {
			fmt.Fprintf(os.Stderr, "\033[33m[%s]\033[0m up to date\n", resolved)
			return nil
		}
		defer func() {
			if err == nil {
				_ = writeChecksum(r.tf.Dir, resolved, checksum)
			}
		}()
	}

	// Build environment
	env := r.buildEnv(task, vars)

	// If the task has a single cmd field, use that
	if task.Cmd.Cmd != "" {
		return r.runCmd(name, r.expandVars(task.Cmd.Cmd, vars, cliArgs), dir, env)
	}
	if task.Cmd.Task != "" {
		return r.Run(task.Cmd.Task, cliArgs)
	}

	// Execute commands
	for _, cmd := range task.Cmds {
		if cmd.Task != "" {
			if err := r.Run(cmd.Task, cliArgs); err != nil {
				return err
			}
			continue
		}
		if err := r.runCmd(name, r.expandVars(cmd.Cmd, vars, cliArgs), dir, env); err != nil {
			return err
		}
	}

	return nil
}

func (r *Runner) taskDir(task Task) string {
	if task.Dir != "" {
		if filepath.IsAbs(task.Dir) {
			return task.Dir
		}
		return filepath.Join(r.tf.Dir, task.Dir)
	}
	return r.tf.Dir
}

func (r *Runner) resolveVars(task Task) map[string]string {
	resolved := make(map[string]string)

	// Built-in vars
	resolved["TASKFILE_DIR"] = r.taskDir(task)

	// Global vars
	for k, v := range r.tf.Vars {
		resolved[k] = r.resolveVar(v, r.tf.Dir)
	}

	// Task vars override
	for k, v := range task.Vars {
		resolved[k] = r.resolveVar(v, r.taskDir(task))
	}

	return resolved
}

func (r *Runner) resolveVar(v Var, dir string) string {
	if v.Sh != "" {
		cmd := exec.Command("sh", "-c", v.Sh)
		cmd.Dir = dir
		out, err := cmd.Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	return v.Value
}

func (r *Runner) buildEnv(task Task, vars map[string]string) []string {
	env := make([]string, len(r.env))
	copy(env, r.env)

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

func (r *Runner) expandVars(s string, vars map[string]string, cliArgs string) string {
	s = strings.ReplaceAll(s, "{{.CLI_ARGS}}", cliArgs)
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{{."+k+"}}", v)
	}
	// Expand HOME and similar
	s = os.Expand(s, func(key string) string {
		if val, ok := vars[key]; ok {
			return val
		}
		return os.Getenv(key)
	})
	return s
}

func (r *Runner) runCmd(taskName, command, dir string, env []string) error {
	fmt.Fprintf(os.Stderr, "\033[32m[%s]\033[0m %s\n", taskName, command)

	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("\033[31mtask: Failed to run task %q: %w\033[0m", taskName, err)
	}
	return nil
}
