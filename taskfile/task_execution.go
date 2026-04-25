package taskfile

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

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
		if err := r.ShellRunner.Run(ShellCommand{
			Kind:     ShellCommandPrecondition,
			TaskName: taskName,
			Command:  pre.Sh,
			Dir:      dir,
			Env:      env,
		}); err != nil {
			if pre.Msg != "" {
				return fmt.Errorf("task %q: %s", taskName, pre.Msg)
			}
			return fmt.Errorf("task %q: precondition failed: %s", taskName, pre.Sh)
		}
	}
	return nil
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
		if err := r.runShellTaskCommand(taskName, expanded, dir, env, useOpRun); err != nil {
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

// runShellTaskCommand executes a task command through the configured shell runner.
func (r *Runner) runShellTaskCommand(taskName, command, dir string, env []string, useOpRun bool) error {
	err := r.ShellRunner.Run(ShellCommand{
		Kind:     ShellCommandTask,
		TaskName: taskName,
		Command:  command,
		Dir:      dir,
		Env:      env,
		UseOpRun: useOpRun,
		Stdin:    os.Stdin,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
	})
	if err != nil {
		return fmt.Errorf("task %q: %w", taskName, err)
	}
	return nil
}
