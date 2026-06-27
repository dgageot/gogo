package cops

import (
	"go/ast"
	"go/types"

	"github.com/dgageot/rubocop-go/cop"
)

// NewCloneStashedEnv returns a cop that flags assignments to a
// ShellCommand.Env field whose right-hand side is not a slices.Clone call.
// AGENTS.md requires cloning any slice stashed on ShellCommand.Env (see
// cloneShellCommand in tests): the runner reuses env slices across calls,
// so aliasing one into a recorded command lets a later mutation corrupt the
// record — a classic shared-backing-array bug. Building a fresh value with
// slices.Clone(...) is the sanctioned escape hatch.
func NewCloneStashedEnv() *cop.Func {
	return cop.New(cop.Meta{
		Name:        "Gogo/CloneStashedEnv",
		Description: "Clone slices stashed on ShellCommand.Env with slices.Clone (see AGENTS.md)",
		Severity:    cop.Warning,
	}, func(p *cop.Pass) {
		if p.Info == nil {
			return
		}
		p.ForEachAssign(func(a *ast.AssignStmt) {
			for i, lhs := range a.Lhs {
				sel, ok := lhs.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Env" {
					continue
				}
				if !isShellCommandValue(p.Info.TypeOf(sel.X)) {
					continue
				}
				if i >= len(a.Rhs) || isClonedSlice(a.Rhs[i]) {
					continue
				}
				p.Report(a, "assigning to ShellCommand.Env without slices.Clone aliases the caller's slice — clone it before stashing")
			}
		})
	}, cop.WithTypes())
}

// isShellCommandValue reports whether t is taskfile.ShellCommand (or a
// pointer to it). The receiver of the assignment must be a ShellCommand for
// the clone rule to apply — assignments to, say, exec.Cmd.Env are unrelated.
func isShellCommandValue(t types.Type) bool {
	if t == nil {
		return false
	}
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok || named.Obj() == nil {
		return false
	}
	return named.Obj().Name() == "ShellCommand"
}

// isClonedSlice reports whether expr is a slices.Clone(...) call, the
// sanctioned way to stash an independent copy of an env slice.
func isClonedSlice(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	return cop.IsCallTo(call, "slices", "Clone")
}
