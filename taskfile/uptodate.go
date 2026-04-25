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
