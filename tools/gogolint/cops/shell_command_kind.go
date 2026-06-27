package cops

import (
	"go/ast"

	"github.com/dgageot/rubocop-go/cop"
)

// NewShellCommandKindRequired returns a cop that flags ShellCommand
// composite literals that omit the Kind field. AGENTS.md requires every
// shell invocation to be tagged with the right ShellCommandKind (Task,
// Precondition, Status, or Var): captureExecs filters on it and the op-run
// masking logic keys off it, so an untagged command silently falls into the
// zero-value bucket. Test files are exempt — fixtures often build partial
// literals on purpose.
func NewShellCommandKindRequired() *cop.Func {
	return cop.New(cop.Meta{
		Name:        "Gogo/ShellCommandKindRequired",
		Description: "ShellCommand literals must set Kind (see AGENTS.md)",
		Severity:    cop.Warning,
	}, func(p *cop.Pass) {
		ast.Inspect(p.File, func(n ast.Node) bool {
			cl, ok := n.(*ast.CompositeLit)
			if !ok || !isShellCommandType(cl.Type) {
				return true
			}
			// Only keyed literals carry named fields; a positional literal
			// (no KeyValueExpr elements) would set every field including Kind.
			if len(cl.Elts) == 0 {
				return true
			}
			if _, ok := cop.CompositeLitField(cl, "Kind"); !ok {
				p.Report(cl, "ShellCommand literal omits Kind — tag it Task/Precondition/Status/Var")
			}
			return true
		})
	}, cop.WithScope(func(p *cop.Pass) bool { return !p.IsTestFile() }))
}

// isShellCommandType reports whether expr names the ShellCommand type,
// either bare (ShellCommand, inside package taskfile) or qualified
// (taskfile.ShellCommand, from another package).
func isShellCommandType(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name == "ShellCommand"
	case *ast.SelectorExpr:
		return t.Sel.Name == "ShellCommand"
	}
	return false
}
