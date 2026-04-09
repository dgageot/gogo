package taskfile

import (
	"fmt"
	"os"
	"time"
)

// Watch runs the named task, then polls its sources and re-runs when they change.
func (r *Runner) Watch(name, cliArgs string, interval time.Duration) error {
	resolved, ok := r.resolveTaskName(name)
	if !ok {
		return fmt.Errorf("task %q not found", name)
	}

	task := r.tf.Tasks[resolved]
	if len(task.Sources) == 0 {
		return fmt.Errorf("task %q has no sources, cannot watch", name)
	}

	dir := r.taskDir(&task)

	// Run once immediately
	if err := r.Run(name, cliArgs); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}

	// Track checksum after initial run to avoid immediate re-run
	lastChecksum, err := sourcesChecksum(dir, task.Sources)
	if err != nil {
		return fmt.Errorf("computing sources checksum: %w", err)
	}

	for {
		time.Sleep(interval)

		changed, newChecksum, err := r.hasSourcesChanged(dir, task.Sources, lastChecksum)
		if err != nil {
			return err
		}
		if !changed {
			continue
		}

		logTask(colorYellow, resolved, "sources changed, re-running...")

		if err := r.Run(name, cliArgs); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}

		lastChecksum = newChecksum
	}
}

// hasSourcesChanged computes the sources checksum and reports whether it differs.
func (r *Runner) hasSourcesChanged(dir string, sources []string, lastChecksum string) (bool, string, error) {
	checksum, err := sourcesChecksum(dir, sources)
	if err != nil {
		return false, "", fmt.Errorf("computing sources checksum: %w", err)
	}
	return checksum != lastChecksum, checksum, nil
}
