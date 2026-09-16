package taskfile

import "fmt"

// linkRun tracks actual calls, not the static graph: skipped conditions must
// stay skipped. Tracking waits across goroutines also catches parallel cycles.
func (r *Runner) linkRun(parent, child *taskRun) (func(), error) {
	if parent == nil {
		return func() {}, nil
	}
	r.graphMu.Lock()
	defer r.graphMu.Unlock()
	if r.runReaches(child, parent, make(map[*taskRun]bool)) {
		return nil, fmt.Errorf("task cycle involving %q and %q", parent.name, child.name)
	}
	if r.waits == nil {
		r.waits = make(map[*taskRun]map[*taskRun]int)
	}
	if r.waits[parent] == nil {
		r.waits[parent] = make(map[*taskRun]int)
	}
	r.waits[parent][child]++
	return func() {
		r.graphMu.Lock()
		defer r.graphMu.Unlock()
		r.waits[parent][child]--
		if r.waits[parent][child] == 0 {
			delete(r.waits[parent], child)
		}
		if len(r.waits[parent]) == 0 {
			delete(r.waits, parent)
		}
	}, nil
}

func (r *Runner) runReaches(from, target *taskRun, seen map[*taskRun]bool) bool {
	if from == target {
		return true
	}
	if seen[from] {
		return false
	}
	seen[from] = true
	for next := range r.waits[from] {
		if r.runReaches(next, target, seen) {
			return true
		}
	}
	return false
}
