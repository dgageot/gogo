package taskfile

import (
	"maps"
	"slices"
)

// suggestionDistance caps how typo-tolerant suggestions are. Two edits is
// enough to catch the common slips ('buld' → 'build', 'tetss' → 'tests')
// without proposing wildly different names for genuine typos like 'xyz'.
const suggestionDistance = 2

// suggestionLimit caps the number of names returned with the "not found"
// error. We pick a single best match: showing several near-equidistant
// alternatives is more noise than help when the user just wants the one
// likely fix.
const suggestionLimit = 1

// suggestTasks returns up to suggestionLimit task names that are close to
// the user's input by Levenshtein distance. Internal tasks are excluded
// (they're invisible everywhere else), and aliases that point at the same
// task are deduplicated so a hit via an alias doesn't push the task name
// itself out of the result set.
func (r *Runner) suggestTasks(name string) []string {
	if name == "" {
		return nil
	}

	best := make(map[string]int) // task name → best distance across name and aliases
	consider := func(label, taskName string) {
		if IsInternalTask(taskName) {
			return
		}
		d := levenshtein(name, label)
		if d > suggestionDistance {
			return
		}
		if prev, ok := best[taskName]; !ok || d < prev {
			best[taskName] = d
		}
	}

	for taskName := range r.tf.Tasks {
		consider(taskName, taskName)
	}
	for alias, taskName := range r.aliases {
		consider(alias, taskName)
	}

	// Closest first; the alphabetical pre-sort breaks distance ties.
	names := slices.Sorted(maps.Keys(best))
	slices.SortStableFunc(names, func(a, b string) int { return best[a] - best[b] })
	if len(names) > suggestionLimit {
		names = names[:suggestionLimit]
	}
	return names
}

// levenshtein returns the edit distance between a and b — the minimum number
// of single-character insertions, deletions, or substitutions needed to turn
// one into the other. A single rolling row plus one scalar holding the
// previous diagonal keeps memory at O(min(len(a), len(b))).
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	if len(ra) < len(rb) {
		ra, rb = rb, ra
	}

	row := make([]int, len(rb)+1)
	for j := range row {
		row[j] = j
	}

	for i := 1; i <= len(ra); i++ {
		diag := row[0] // dp[i-1][0] before we overwrite it
		row[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			diag, row[j] = row[j], min(row[j]+1, row[j-1]+1, diag+cost)
		}
	}

	return row[len(rb)]
}
