package taskfile

import (
	"fmt"
	"os"
	"time"
)

// Watch runs the named task, then polls its sources and re-runs when they change.
func (r *Runner) Watch(name string, cliArgs string, interval time.Duration) error {
	resolved, ok := r.resolveTaskName(name)
	if !ok {
		return fmt.Errorf("task %q not found", name)
	}

	task := r.tf.Tasks[resolved]
	if len(task.Sources) == 0 {
		return fmt.Errorf("task %q has no sources, cannot watch", name)
	}

	dir := r.taskDir(task)
	var lastChecksum string

	for {
		checksum, err := sourcesChecksum(dir, task.Sources)
		if err != nil {
			return fmt.Errorf("computing sources checksum: %w", err)
		}

		if checksum != lastChecksum {
			if lastChecksum != "" {
				fmt.Fprintf(os.Stderr, "\033[33m[%s]\033[0m sources changed, re-running...\n", resolved)
			}

			// Run the task, but don't stop watching on failure
			if err := r.Run(name, cliArgs); err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
			}

			lastChecksum = checksum
		}

		time.Sleep(interval)
	}
}
