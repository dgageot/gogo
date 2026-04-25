package taskfile

import (
	"fmt"
	"path/filepath"
	"strings"
)

// resolveTaskName finds the actual task name, trying the exact name first,
// then aliases, then prefixing with the namespace matching the current working directory.
func (r *Runner) resolveTaskName(name string) (string, bool) {
	if _, ok := r.tf.Tasks[name]; ok {
		return name, true
	}

	if taskName, ok := r.aliases[name]; ok {
		return taskName, true
	}

	if ns, ok := r.cwdNamespace(); ok {
		if _, ok := r.tf.Tasks[ns+":"+name]; ok {
			return ns + ":" + name, true
		}
	}

	// Try stripping a self-prefix that matches the taskfile root's basename.
	// This lets "proxy:deploy-prod" work when running from a sub-Taskfile at
	// proxy/ — the same name also works when the parent Taskfile is the root.
	if prefix, suffix, ok := strings.Cut(name, ":"); ok && prefix == filepath.Base(r.tf.Dir) {
		if resolved, ok := r.resolveTaskName(suffix); ok {
			return resolved, true
		}
	}

	return name, false
}

// cwdNamespace returns the most specific namespace whose directory contains
// the runner's current working directory. Used to let users invoke tasks by
// their short name when cwd sits under an included Taskfile.
func (r *Runner) cwdNamespace() (string, bool) {
	var bestDir, bestNS string
	for dir, ns := range r.tf.Namespaces {
		if !strings.HasPrefix(r.cwd+string(filepath.Separator), dir+string(filepath.Separator)) {
			continue
		}
		if len(dir) > len(bestDir) {
			bestDir = dir
			bestNS = ns
		}
	}
	return bestNS, bestNS != ""
}

// resolveTask finds a task by name and returns its resolved name.
func (r *Runner) resolveTask(name string) (string, error) {
	resolved, ok := r.resolveTaskName(name)
	if !ok {
		return "", fmt.Errorf("task %q not found", name)
	}
	return resolved, nil
}
