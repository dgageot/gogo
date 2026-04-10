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
	if err := r.Run(resolved, cliArgs); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}

	// Track checksum after initial run to avoid immediate re-run
	lastChecksum, err := sourcesChecksum(dir, task.Sources)
	if err != nil {
		return fmt.Errorf("computing sources checksum: %w", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		newChecksum, err := sourcesChecksum(dir, task.Sources)
		if err != nil {
			return fmt.Errorf("computing sources checksum: %w", err)
		}
		if newChecksum == lastChecksum {
			continue
		}

		logTask(colorYellow, resolved, "sources changed, re-running...")

		if err := r.Run(resolved, cliArgs); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}

		lastChecksum = newChecksum
	}

	return nil
}
