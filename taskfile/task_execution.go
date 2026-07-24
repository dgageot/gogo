package taskfile

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
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

// checkRequires validates that all required vars and env are set. Built-in
// variables (e.g. {{.GIT_COMMIT}}) satisfy a `requires.vars:` entry as long
// as builtin reports them known — even when the value is empty, since
// "missing exact-match tag" or "clean working tree" are valid empty results.
func checkRequires(taskName string, task *Task, vars map[string]string, builtin func(string) (string, bool)) error {
	for _, name := range task.Requires.Vars {
		if _, ok := vars[name]; ok {
			continue
		}
		if builtin != nil {
			if _, ok := builtin(name); ok {
				continue
			}
		}
		return fmt.Errorf("task %q requires variable %q to be set", taskName, name)
	}
	for _, name := range task.Requires.Env {
		if _, ok := os.LookupEnv(name); !ok {
			return fmt.Errorf("task %q requires environment variable %q to be set", taskName, name)
		}
	}
	return nil
}

// checkPreconditions runs all precondition shell commands for a task.
// Preconditions see the same environment as the task's commands, including
// op:// secret resolution: when the built env carries op:// references we
// wrap the precondition in `op run` too, so a check that reads a secret
// (e.g. `test -n "$TOKEN"`) sees the resolved value rather than the literal
// op:// URI. If any command fails, it returns an error with the
// precondition's message or a default message.
func (r *Runner) checkPreconditions(taskName string, task *Task, dir string, env []string) error {
	useOpRun := hasOpSecrets(env)
	for _, pre := range task.Preconditions {
		if err := r.ShellRunner.Run(ShellCommand{
			Kind:     ShellCommandPrecondition,
			TaskName: taskName,
			Command:  pre.Sh,
			Dir:      dir,
			Env:      env,
			UseOpRun: useOpRun,
		}); err != nil {
			if pre.Msg != "" {
				return fmt.Errorf("task %q: %s", taskName, pre.Msg)
			}
			return fmt.Errorf("task %q: precondition failed: %s", taskName, pre.Sh)
		}
	}
	return nil
}

// conditionMet reports whether an `if:` condition allows execution. The
// condition is a shell command run with the same working dir and env as the
// task's own commands (wrapped in `op run` when the env carries op://
// secrets, so a check may read a resolved secret). Its exit status is the
// answer: zero runs, non-zero skips — a skip is never an error.
func (r *Runner) conditionMet(taskName, condition, dir string, env []string, useOpRun bool) bool {
	return r.ShellRunner.Run(ShellCommand{
		Kind:     ShellCommandCondition,
		TaskName: taskName,
		Command:  condition,
		Dir:      dir,
		Env:      env,
		UseOpRun: useOpRun,
	}) == nil
}

// Run executes the named task. Extra vars (from task call sites) override task-level vars.
func (r *Runner) Run(name, cliArgs string, extraVars ...map[string]Var) error {
	resolved, err := r.resolveTask(name)
	if err != nil {
		return err
	}

	// Each call site with extra vars is a distinct execution — bypass memoization.
	if hasExtraVars(extraVars) {
		return r.run(resolved, cliArgs, extraVars, nil)
	}

	entry, _ := r.runs.LoadOrStore(resolved, &taskRun{})
	tr, ok := entry.(*taskRun)
	if !ok {
		return fmt.Errorf("internal error: unexpected runs entry type %T for task %q", entry, resolved)
	}
	return tr.do(func() error {
		return r.run(resolved, cliArgs, nil, nil)
	})
}

// runSubTask is the entry point for `cmds: - task: X` calls. Each sub-task
// invocation propagates the parent task's resolved env down so child tasks
// see what the parent declared in its `env:` block (matching shell-function
// semantics). Memoization is always bypassed because two parents calling the
// same child with different env are genuinely different executions.
func (r *Runner) runSubTask(name, cliArgs string, extraVars map[string]Var, parentEnv []string) error {
	resolved, err := r.resolveTask(name)
	if err != nil {
		return err
	}
	var ev []map[string]Var
	if len(extraVars) > 0 {
		ev = []map[string]Var{extraVars}
	}
	return r.run(resolved, cliArgs, ev, parentEnv)
}

// hasExtraVars reports whether the variadic extraVars carries any overrides.
func hasExtraVars(extraVars []map[string]Var) bool {
	return len(extraVars) > 0 && len(extraVars[0]) > 0
}

// run executes a task's body. Deduplication is handled by Run; this method
// always runs the task, so recursive calls from runCmds must go through Run
// (or runSubTask, which threads the parent env down).
func (r *Runner) run(resolved, cliArgs string, extraVars []map[string]Var, parentEnv []string) error {
	task := r.tf.Tasks[resolved]

	if !matchesPlatform(task.Platforms) {
		r.logTask(colorYellow, resolved, "skipped (platform mismatch)")
		return nil
	}

	// `if:` gates the whole task — prompt and deps included — so a skipped
	// task leaves the system untouched. The env is composed early just for
	// the check (buildEnv is pure composition, no commands run); tasks
	// without a condition keep the usual phase order.
	if task.If != "" {
		dir := r.taskDir(&task)
		env, err := r.buildEnv(&task, dir, parentEnv)
		if err != nil {
			return err
		}
		if !r.conditionMet(resolved, task.If, dir, env, hasOpSecrets(env)) {
			r.logTask(colorYellow, resolved, "skipped (condition not met)")
			return nil
		}
	}

	// Prompt before anything runs — deps included — so declining leaves the
	// system untouched.
	if task.Prompt != "" {
		if err := r.confirmPrompt(resolved, task.Prompt); err != nil {
			return err
		}
	}

	if err := r.runDeps(task.Deps); err != nil {
		return err
	}

	dir := r.taskDir(&task)

	vars, unusedVars, err := r.resolveAllVars(resolved, &task, dir, extraVars)
	if err != nil {
		return err
	}
	for _, name := range unusedVars {
		r.logTask(colorYellow, resolved, fmt.Sprintf("warning: variable %q is declared but not used", name))
	}

	if err := checkRequires(resolved, &task, vars, r.builtinLookup); err != nil {
		return err
	}

	env, err := r.buildEnv(&task, dir, parentEnv)
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
	// `status:` probes the desired end state. With no sources it decides
	// alone; with sources both must agree, so either signal forces a run.
	// It's only consulted when it can still flip the answer — changed
	// sources already force a run, no point shelling out on top.
	if !r.Force && len(task.Status) > 0 && (len(task.Sources) == 0 || upToDate) {
		upToDate = r.statusUpToDate(resolved, &task, dir, env)
	}
	if upToDate {
		r.logTask(colorYellow, resolved, "up to date")
		return nil
	}

	if err := r.runCmds(resolved, task.Cmds, vars, cliArgs, dir, env, hasOpSecrets(env), task.Silent); err != nil {
		return err
	}

	// Don't persist the checksum on a dry run: nothing actually executed, so
	// recording "done" would make the next real run skip the task as up to
	// date even though it never built anything.
	if checksum != "" && !r.DryRun {
		if err := writeChecksum(r.tf.Dir, resolved, checksum); err != nil {
			// Best-effort: a failed write means we'll re-run next time, but
			// silently swallowing it left users wondering why a clean build
			// kept rebuilding. Surface it as a warning instead.
			r.logTask(colorYellow, resolved, fmt.Sprintf("warning: failed to persist checksum: %v", err))
		}
	}
	return nil
}

// runCmds executes a list of commands in sequence. When silent is true, the
// per-cmd "[task] cmd" log line is suppressed; the command itself still runs
// and its own stdout/stderr are unaffected. Deferred commands (`defer:`)
// registered before a failure still run after the task's body, in reverse
// registration order — they exist for cleanup, so a failed cmd must not skip
// them.
func (r *Runner) runCmds(taskName string, cmds []Cmd, vars map[string]string, cliArgs, dir string, env []string, useOpRun, silent bool) error {
	var deferred []string
	defer func() {
		r.runDeferred(taskName, deferred, dir, env, useOpRun, silent)
	}()

	for _, cmd := range cmds {
		// A per-entry `if:` gates every kind of entry uniformly: a defer whose
		// condition fails is never registered, a task: sub-call is never made.
		// Unlike task-level `if:`, the condition here sees resolved vars.
		if cmd.If != "" {
			condition := expandVars(cmd.If, vars, cliArgs, r.builtinLookup)
			if !r.conditionMet(taskName, condition, dir, env, useOpRun) {
				if !silent {
					r.logTask(colorYellow, taskName, "skipped (condition not met): "+condition)
				}
				continue
			}
		}
		if cmd.Defer != "" {
			deferred = append(deferred, expandVars(cmd.Defer, vars, cliArgs, r.builtinLookup))
			continue
		}
		if cmd.Task != "" {
			// `task: X` sub-calls inherit the parent's resolved env so a
			// task-level `env:` block flows down without the user having to
			// shell out to `gogo` to plumb env through. Child env still wins
			// per-key (composed inside buildEnv).
			if err := r.runSubTask(cmd.Task, cliArgs, cmd.Vars, env); err != nil {
				return err
			}
			continue
		}

		// Expand once and reuse for both the log line and execution so the two
		// can never drift apart (and we don't resolve built-in vars twice).
		if r.DryRun {
			if !silent {
				r.logTask(colorGreen, taskName, expandVars(cmd.Cmd, vars, cliArgs, r.builtinLookup))
			}
			continue
		}

		expanded := expandVars(cmd.Cmd, vars, cliArgs, r.builtinLookup)
		if !silent {
			r.logTask(colorGreen, taskName, expanded)
		}
		if err := r.runShellTaskCommand(taskName, expanded, dir, env, useOpRun); err != nil {
			if cmd.IgnoreError {
				r.logTask(colorYellow, taskName, fmt.Sprintf("warning: command failed (ignored): %v", err))
				continue
			}
			return err
		}
	}
	return nil
}

// runDeferred executes deferred commands in reverse registration order,
// mirroring Go's defer semantics. A deferred command failure is logged as a
// warning rather than returned: cleanup must not mask the task's own result,
// and later defers must still run.
func (r *Runner) runDeferred(taskName string, cmds []string, dir string, env []string, useOpRun, silent bool) {
	for _, cmd := range slices.Backward(cmds) {
		if !silent {
			r.logTask(colorGreen, taskName, cmd)
		}
		if r.DryRun {
			continue
		}
		if err := r.runShellTaskCommand(taskName, cmd, dir, env, useOpRun); err != nil {
			r.logTask(colorYellow, taskName, fmt.Sprintf("warning: deferred command failed: %v", err))
		}
	}
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

func (r *Runner) logTask(color, name, msg string) {
	logTask(r.IO.Stderr, color, name, msg)
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
		Stdin:    r.IO.Stdin,
		Stdout:   r.IO.Stdout,
		Stderr:   r.IO.Stderr,
	})
	if err != nil {
		return fmt.Errorf("task %q: %w", taskName, err)
	}
	return nil
}
