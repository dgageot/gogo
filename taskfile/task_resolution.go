package taskfile

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// resolveTask resolves user input — an exact name, alias, namespace shortcut,
// or a unique segment-wise prefix — to a task name. Ambiguous prefixes are
// rejected so the wrong task is never silently invoked.
func (r *Runner) resolveTask(name string) (string, error) {
	if resolved, ok := r.resolveTaskName(name); ok {
		return resolved, nil
	}
	switch matches := r.prefixMatches(name); len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("task %q not found", name)
	default:
		return "", fmt.Errorf("task %q is ambiguous (matches: %s)", name, strings.Join(matches, ", "))
	}
}

// resolveTaskName returns the canonical task name for input that matches a
// task or alias exactly under any of the name's candidate interpretations
// (literal, cwd-namespaced, self-prefix stripped). It does NOT perform
// prefix matching — callers wanting that should use resolveTask.
func (r *Runner) resolveTaskName(name string) (string, bool) {
	for _, cand := range r.nameCandidates(name) {
		if _, ok := r.tf.Tasks[cand]; ok {
			return cand, true
		}
		if taskName, ok := r.aliases[cand]; ok {
			return taskName, true
		}
	}
	return "", false
}

// prefixMatches returns sorted task names that match name segment-by-segment:
// each colon-separated piece of the input must be a non-empty prefix of the
// corresponding piece of the task name, and the number of segments must
// match. That lets 'sub:i' resolve to 'sub:install' and 's:i' to the same,
// mirroring the way editors filter file paths. Candidate interpretations
// from nameCandidates are tried in order so cwd-namespace and self-prefix
// rules carry over. Internal tasks ('_'-prefixed local segment) are skipped
// so prefix matching never lands on a private helper, and an empty input
// (or any empty segment) matches nothing.
func (r *Runner) prefixMatches(name string) []string {
	if name == "" {
		return nil
	}
	for _, cand := range r.nameCandidates(name) {
		candSegs := strings.Split(cand, ":")
		if slices.Contains(candSegs, "") {
			continue
		}
		seen := make(map[string]bool)
		var out []string
		for taskName := range r.tf.Tasks {
			if IsInternalTask(taskName) {
				continue
			}
			if segmentPrefixMatch(taskName, candSegs) {
				if !seen[taskName] {
					seen[taskName] = true
					out = append(out, taskName)
				}
			}
		}
		for alias, taskName := range r.aliases {
			if IsInternalTask(taskName) {
				continue
			}
			if segmentPrefixMatch(alias, candSegs) {
				if !seen[taskName] {
					seen[taskName] = true
					out = append(out, taskName)
				}
			}
		}
		if len(out) > 0 {
			slices.Sort(out)
			return out
		}
	}
	return nil
}

// segmentPrefixMatch reports whether each piece of candSegs is a prefix of
// the corresponding colon-separated piece of taskName. The segment counts
// must agree, so 'sub' on its own no longer matches 'sub:install' — the
// user has to type something into every namespace level they want to span.
func segmentPrefixMatch(taskName string, candSegs []string) bool {
	taskSegs := strings.Split(taskName, ":")
	if len(taskSegs) != len(candSegs) {
		return false
	}
	for i, s := range candSegs {
		if !strings.HasPrefix(taskSegs[i], s) {
			return false
		}
	}
	return true
}

// nameCandidates lists the interpretations of name, in priority order, that
// resolution should try. The literal name comes first; then — when cwd sits
// inside an included namespace — the cwd-namespaced form; then, recursively,
// the name with a self-prefix stripped (matching the task file root's
// basename). Both exact and prefix matching walk this same list.
func (r *Runner) nameCandidates(name string) []string {
	base := filepath.Base(r.tf.Dir)
	ns, hasNS := r.cwdNamespace()
	var out []string
	for cur := name; ; {
		out = append(out, cur)
		if hasNS {
			out = append(out, ns+":"+cur)
		}
		prefix, suffix, hasColon := strings.Cut(cur, ":")
		if !hasColon || prefix != base {
			return out
		}
		cur = suffix
	}
}

// cwdNamespace returns the most specific namespace whose directory contains
// the runner's current working directory. Used to let users invoke tasks by
// their short name when cwd sits under an included task file.
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

// IsInternalTask reports whether a task name is internal (its local segment
// starts with '_'). Only the part after the last colon is checked so
// 'ns:_helper' is internal but 'ns:public' is not. Internal tasks are
// hidden from --list, completion, and prefix matching.
func IsInternalTask(name string) bool {
	if i := strings.LastIndex(name, ":"); i >= 0 {
		name = name[i+1:]
	}
	return strings.HasPrefix(name, "_")
}
