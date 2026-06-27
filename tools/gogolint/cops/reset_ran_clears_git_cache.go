package cops

import (
	"go/ast"

	"github.com/dgageot/rubocop-go/cop"
)

// NewResetRanClearsGitCache returns a cop that verifies Runner.ResetRan
// clears the git-vars cache, not just the memoized task runs. AGENTS.md:
// "Watch mode must call ResetRan between iterations — it also clears the
// gitVars cache so {{.GIT_DIRTY}} re-evaluates after each edit." If ResetRan
// resets r.runs but forgets r.gitOnce / r.gitVars, watch mode would serve a
// stale dirty/HEAD value forever — a silent correctness bug. The cop checks
// that the method assigns to all three fields.
func NewResetRanClearsGitCache() *cop.Func {
	return cop.New(cop.Meta{
		Name:        "Gogo/ResetRanClearsGitCache",
		Description: "Runner.ResetRan must clear runs, gitOnce, and gitVars (see AGENTS.md watch mode)",
		Severity:    cop.Warning,
	}, func(p *cop.Pass) {
		p.ForEachFunc(func(fn *ast.FuncDecl) {
			if fn.Recv == nil || fn.Name == nil || fn.Name.Name != "ResetRan" || fn.Body == nil {
				return
			}
			assigned := receiverFieldsAssigned(fn)
			for _, field := range []string{"runs", "gitOnce", "gitVars"} {
				if !assigned[field] {
					p.Reportf(fn.Name, "ResetRan does not reset r.%s — watch mode would observe stale state", field)
				}
			}
		})
	}, cop.WithScope(cop.OnlyFile("taskfile/runner.go")))
}

// receiverFieldsAssigned returns the set of receiver field names that fn
// assigns to (the "f" in `r.f = ...`), regardless of the receiver's name.
func receiverFieldsAssigned(fn *ast.FuncDecl) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range assign.Lhs {
			if sel, ok := lhs.(*ast.SelectorExpr); ok {
				out[sel.Sel.Name] = true
			}
		}
		return true
	})
	return out
}
