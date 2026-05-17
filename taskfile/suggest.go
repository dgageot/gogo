package taskfile

import (
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

	type candidate struct {
		display  string // shown to the user (task name, never the alias)
		distance int
	}

	seen := make(map[string]int) // task name → best distance seen so far
	var results []candidate

	consider := func(label, taskName string) {
		if IsInternalTask(taskName) {
			return
		}
		d := levenshtein(name, label)
		if d > suggestionDistance {
			return
		}
		if prev, ok := seen[taskName]; ok && prev <= d {
			return
		}
		seen[taskName] = d
		results = append(results, candidate{display: taskName, distance: d})
	}

	for taskName := range r.tf.Tasks {
		consider(taskName, taskName)
	}
	for alias, taskName := range r.aliases {
		consider(alias, taskName)
	}

	// Drop entries that were superseded by a closer match for the same task.
	results = slices.DeleteFunc(results, func(c candidate) bool {
		return seen[c.display] != c.distance
	})

	slices.SortFunc(results, func(a, b candidate) int {
		if a.distance != b.distance {
			return a.distance - b.distance
		}
		switch {
		case a.display < b.display:
			return -1
		case a.display > b.display:
			return 1
		default:
			return 0
		}
	})

	if len(results) > suggestionLimit {
		results = results[:suggestionLimit]
	}

	out := make([]string, len(results))
	for i, c := range results {
		out[i] = c.display
	}
	return out
}

// levenshtein returns the edit distance between a and b — the minimum number
// of single-character insertions, deletions, or substitutions needed to turn
// one into the other. The implementation uses two rolling rows so memory
// stays O(min(len(a), len(b))) even for long task names.
func levenshtein(a, b string) int {
	ra := []rune(a)
	rb := []rune(b)
	if len(ra) < len(rb) {
		ra, rb = rb, ra
	}
	if len(rb) == 0 {
		return len(ra)
	}

	prev := make([]int, len(rb)+1)
	curr := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(ra); i++ {
		curr[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min(
				prev[j]+1,      // deletion
				curr[j-1]+1,    // insertion
				prev[j-1]+cost, // substitution
			)
		}
		prev, curr = curr, prev
	}

	return prev[len(rb)]
}
