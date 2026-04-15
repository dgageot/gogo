package taskfile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"slices"
	"time"
)

const minWatchInterval = 10 * time.Millisecond

// dirPatterns pairs a directory with its source glob patterns.
type dirPatterns struct {
	Dir      string
	Patterns []string
}

// collectSources gathers all source patterns from the task and its dependencies (recursively),
// preserving each task's working directory.
func (r *Runner) collectSources(taskName string, visited map[string]struct{}) []dirPatterns {
	if _, ok := visited[taskName]; ok {
		return nil
	}
	visited[taskName] = struct{}{}

	task, ok := r.tf.Tasks[taskName]
	if !ok {
		return nil
	}

	var result []dirPatterns
	if len(task.Sources) > 0 {
		result = append(result, dirPatterns{
			Dir:      r.taskDir(&task),
			Patterns: slices.Clone([]string(task.Sources)),
		})
	}
	for _, dep := range task.Deps {
		resolved, ok := r.resolveTaskName(dep.Task)
		if !ok {
			continue
		}
		result = append(result, r.collectSources(resolved, visited)...)
	}

	return result
}

// multiSourcesChecksum computes a combined checksum across multiple dir/pattern groups.
func multiSourcesChecksum(groups []dirPatterns) (string, error) {
	h := sha256.New()
	for _, g := range groups {
		checksum, err := sourcesChecksum(g.Dir, g.Patterns)
		if err != nil {
			return "", err
		}
		h.Write([]byte(checksum))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Watch runs the named task, then polls its sources and re-runs when they change.
// It stops gracefully when the context is cancelled.
func (r *Runner) Watch(ctx context.Context, name, cliArgs string, interval time.Duration) error {
	if interval < minWatchInterval {
		return fmt.Errorf("watch interval must be at least %s", minWatchInterval)
	}

	resolved, err := r.resolveTask(name)
	if err != nil {
		return err
	}

	sources := r.collectSources(resolved, make(map[string]struct{}))
	if len(sources) == 0 {
		return fmt.Errorf("task %q has no sources, cannot watch", name)
	}

	// Run once immediately
	if err := r.Run(resolved, cliArgs); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}

	// Track checksum after initial run to avoid immediate re-run
	lastChecksum, err := multiSourcesChecksum(sources)
	if err != nil {
		return fmt.Errorf("computing sources checksum: %w", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}

		newChecksum, err := multiSourcesChecksum(sources)
		if err != nil {
			return fmt.Errorf("computing sources checksum: %w", err)
		}
		if newChecksum == lastChecksum {
			continue
		}
		lastChecksum = newChecksum

		logTask(colorYellow, resolved, "sources changed, re-running...")

		// Reset dedup state so tasks can re-run
		r.ResetRan()

		if err := r.Run(resolved, cliArgs); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}
}
