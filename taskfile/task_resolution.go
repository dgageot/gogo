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
		return "", notFoundError(name, r.suggestTasks(name))
	default:
		return "", fmt.Errorf("task %q is ambiguous (matches: %s)", name, strings.Join(matches, ", "))
	}
}

// TaskNotFoundError is returned when the user asks for a task name that
// doesn't resolve. It's a typed error so callers (notably the CLI) can
// react — for example, by printing the task list alongside the message —
// without parsing the formatted text.
type TaskNotFoundError struct {
	Name        string   // the unresolved input as typed by the user
	Suggestions []string // best-effort 'did you mean' hints (may be empty)
}

// Error renders the message historically produced by resolveTask. The exact
// wording is part of the user-facing contract: tooling and tests grep for
// 'not found' and 'did you mean'.
func (e *TaskNotFoundError) Error() string {
	if len(e.Suggestions) == 0 {
		return fmt.Sprintf("task %q not found", e.Name)
	}
	return fmt.Sprintf("task %q not found, did you mean: %s?", e.Name, strings.Join(e.Suggestions, ", "))
}

// notFoundError formats the error reported when a task name can't be
// resolved. When near-matches exist they're appended as a 'did you mean'
// hint so a typo turns into a one-line fix instead of a hunt through
// `gogo --list`.
func notFoundError(name string, suggestions []string) error {
	return &TaskNotFoundError{Name: name, Suggestions: suggestions}
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
// resolution should try. The root/literal name normally comes first. When the
// CLI promoted an included project to its parent include root, bare names from
// inside that project prefer the cwd namespace so `gogo build` still behaves
// like it did when the sub-project ran standalone.
func (r *Runner) nameCandidates(name string) []string {
	base := filepath.Base(r.tf.Dir)
	ns, hasNS := r.cwdNamespace()
	var out []string
	strippedSelfPrefix := false
	for cur := name; ; {
		if hasNS && r.PreferCwdNamespace && !strippedSelfPrefix && !strings.Contains(cur, ":") {
			out = append(out, ns+":"+cur, cur)
		} else {
			out = append(out, cur)
			if hasNS {
				out = append(out, ns+":"+cur)
			}
		}
		prefix, suffix, hasColon := strings.Cut(cur, ":")
		if !hasColon || prefix != base {
			return out
		}
		strippedSelfPrefix = true
		cur = suffix
	}
}

// NamespaceForDir returns the most specific namespace whose directory contains
// dir. Used to let users invoke tasks by their short name when cwd sits under
// an included task file.
func NamespaceForDir(tf *Config, dir string) (string, bool) {
	var bestDir, bestNS string
	for namespaceDir, ns := range tf.Namespaces {
		if !strings.HasPrefix(dir+string(filepath.Separator), namespaceDir+string(filepath.Separator)) {
			continue
		}
		if len(namespaceDir) > len(bestDir) {
			bestDir = namespaceDir
			bestNS = ns
		}
	}
	return bestNS, bestNS != ""
}

func (r *Runner) cwdNamespace() (string, bool) {
	return NamespaceForDir(r.tf, r.cwd)
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
