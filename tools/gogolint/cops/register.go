// Package cops holds gogo's project-specific rubocop-go cops. Each cop
// encodes an invariant documented in AGENTS.md that the off-the-shelf
// golangci-lint suite cannot express.
package cops

import "github.com/dgageot/rubocop-go/cop"

// All returns every gogo cop, in a stable order.
func All() []cop.Cop {
	return []cop.Cop{
		NewShellViaRunner(),
		NewSortedMapIteration(),
	}
}
