package cops

import (
	"go/ast"

	"github.com/dgageot/rubocop-go/cop"
)

// NewTestifyHelpers returns a cop that flags bare t.Error/t.Errorf/t.Fatal/
// t.Fatalf calls in test files. AGENTS.md mandates testify: require for
// fatal preconditions, assert for non-fatal checks — never bare t.Fatal /
// t.Error for value comparisons. The matcher keys on the receiver
// identifier "t" so the ubiquitous err.Error() string accessor is never
// mistaken for a testing call.
func NewTestifyHelpers() *cop.Func {
	return cop.New(cop.Meta{
		Name:        "Gogo/TestifyHelpers",
		Description: "Use testify require/assert, not bare t.Error*/t.Fatal* (see AGENTS.md)",
		Severity:    cop.Warning,
	}, func(p *cop.Pass) {
		p.ForEachCall(func(call *ast.CallExpr) {
			// Match calls on the conventional testing.T receiver named "t".
			// err.Error() has receiver "err" (and takes no args), so it is
			// never matched here.
			name, ok := cop.CallTo(call, "t", "Error", "Errorf", "Fatal", "Fatalf")
			if !ok {
				return
			}
			p.Reportf(call, "bare t.%s — use require (fatal) or assert (non-fatal) from testify instead", name)
		})
	}, cop.WithScope(func(p *cop.Pass) bool { return p.IsTestFile() }))
}
