package cops

import (
	"go/ast"

	"github.com/dgageot/rubocop-go/cop"
)

// NewNoPrintInLibrary returns a cop that flags fmt.Print/Printf/Println in
// library code. AGENTS.md forbids printing directly from libraries: output
// must go through Runner.logTask or the injected RunnerIO / App.Stdout|Stderr
// so it stays testable and respects the configured streams. forbidigo
// already bans fmt.Print* in tests; this covers the production packages.
// Package main is exempt — it owns the process's stdout (e.g. emitting the
// shell-completion scripts).
func NewNoPrintInLibrary() *cop.Func {
	return cop.New(cop.Meta{
		Name:        "Gogo/NoPrintInLibrary",
		Description: "No fmt.Print* in library code — use logTask or the injected IO (see AGENTS.md)",
		Severity:    cop.Warning,
	}, func(p *cop.Pass) {
		p.ForEachCall(func(call *ast.CallExpr) {
			name, ok := cop.CallTo(call, "fmt", "Print", "Printf", "Println")
			if !ok {
				return
			}
			p.Reportf(call, "fmt.%s in library code — write to the injected IO (RunnerIO / App.Stdout) or use logTask", name)
		})
	}, cop.WithScope(cop.And(
		func(p *cop.Pass) bool { return !p.IsMain() },
		func(p *cop.Pass) bool { return !p.IsTestFile() },
	)))
}
