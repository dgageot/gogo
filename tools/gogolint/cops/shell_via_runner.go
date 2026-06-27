package cops

import (
	"go/ast"

	"github.com/dgageot/rubocop-go/cop"
)

// NewShellViaRunner returns a cop that forbids direct exec.Command /
// exec.CommandContext calls outside the shell layer. AGENTS.md requires
// every shell-out to be routed through Runner.ShellRunner so tests can
// intercept it; the only legitimate callers are the default runner in
// taskfile/shell.go and the foreign-fallback dispatcher in fallback.go.
// Test files are exempt — they may exec a real shell to assert behavior.
func NewShellViaRunner() *cop.Func {
	return cop.New(cop.Meta{
		Name:        "Gogo/ShellViaRunner",
		Description: "Shell out via Runner.ShellRunner, not exec.Command directly (see AGENTS.md)",
		Severity:    cop.Warning,
	}, func(p *cop.Pass) {
		p.ForEachCall(func(call *ast.CallExpr) {
			name, ok := cop.CallTo(call, "exec", "Command", "CommandContext")
			if !ok {
				return
			}
			p.Reportf(call, "exec.%s bypasses Runner.ShellRunner — route shell calls through it so tests can intercept them", name)
		})
	}, cop.WithScope(cop.And(
		cop.Not(cop.OnlyFile("taskfile/shell.go")),
		cop.Not(cop.OnlyFile("fallback.go")),
		func(p *cop.Pass) bool { return !p.IsTestFile() },
	)))
}
