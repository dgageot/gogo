package taskfile

import (
	"fmt"
	"os"
	"slices"
	"time"
)

// collectSources gathers all source patterns from the task and its dependencies (recursively).
func (r *Runner) collectSources(taskName string, visited map[string]bool) []string {
	if visited[taskName] {
		return nil
	}
	visited[taskName] = true

	task, ok := r.tf.Tasks[taskName]
	if !ok {
		return nil
	}

	sources := slices.Clone(task.Sources)
	for _, dep := range task.Deps {
		resolved, ok := r.resolveTaskName(dep.Task)
		if !ok {
			continue
		}
		sources = append(sources, r.collectSources(resolved, visited)...)
	}

	return sources
}

// Watch runs the named task, then polls its sources and re-runs when they change.
func (r *Runner) Watch(name, cliArgs string, interval time.Duration) error {
	resolved, task, err := r.resolveTask(name)
	if err != nil {
		return err
	}

	sources := r.collectSources(resolved, make(map[string]bool))
	if len(sources) == 0 {
		return fmt.Errorf("task %q has no sources, cannot watch", name)
	}

	dir := r.taskDir(&task)

	// Run once immediately
	if err := r.Run(resolved, cliArgs); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}

	// Track checksum after initial run to avoid immediate re-run
	lastChecksum, err := sourcesChecksum(dir, sources)
	if err != nil {
		return fmt.Errorf("computing sources checksum: %w", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		newChecksum, err := sourcesChecksum(dir, sources)
		if err != nil {
			return fmt.Errorf("computing sources checksum: %w", err)
		}
		if newChecksum == lastChecksum {
			continue
		}
		lastChecksum = newChecksum

		logTask(colorYellow, resolved, "sources changed, re-running...")

		if err := r.Run(resolved, cliArgs); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}

	return nil
}
