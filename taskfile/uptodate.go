package taskfile

import "fmt"

// isUpToDate checks if the task sources are unchanged since the last run.
// When generates is set, it checks that all outputs exist and are newer than all sources.
// Otherwise, it falls back to checksum-based comparison.
// Returns whether the task is up-to-date, the current checksum (empty when using generates), and any error.
func (r *Runner) isUpToDate(task *Task, dir, taskName string, force bool) (bool, string, error) {
	if force || len(task.Sources) == 0 {
		return false, "", nil
	}

	sources, err := r.tf.taskSources([]string(task.Sources))
	if err != nil {
		return false, "", fmt.Errorf("resolving sources for task %q: %w", taskName, err)
	}

	// When generates is set, use timestamp-based comparison
	if len(task.Generates) > 0 {
		upToDate, err := outputsNewerThanSources(dir, sources, task.Generates)
		return upToDate, "", err
	}

	checksum, err := sourcesChecksum(dir, sources)
	if err != nil {
		return false, "", fmt.Errorf("computing sources checksum: %w", err)
	}

	// No files matched the patterns: always run.
	if checksum == "" {
		return false, "", nil
	}

	return checksum == readStoredChecksum(r.tf.Dir, taskName), checksum, nil
}

// statusUpToDate reports whether every `status:` command exits 0, meaning
// the task's desired end state already exists and it can be skipped. The
// first failing command short-circuits: one "not done" answer is enough.
// Status commands see the task's full env — including op:// secret
// resolution — exactly like preconditions, so probes can read the same
// credentials the task itself would use.
func (r *Runner) statusUpToDate(taskName string, task *Task, dir string, env []string) bool {
	useOpRun := hasOpSecrets(env)
	for _, sh := range task.Status {
		if err := r.ShellRunner.Run(ShellCommand{
			Kind:     ShellCommandStatus,
			TaskName: taskName,
			Command:  sh,
			Dir:      dir,
			Env:      env,
			UseOpRun: useOpRun,
		}); err != nil {
			return false
		}
	}
	return true
}
