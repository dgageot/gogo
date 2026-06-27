package cops

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"github.com/dgageot/rubocop-go/cop"
)

// NewWrapErrors returns a cop that flags fmt.Errorf calls which interpolate
// an error value without the %w verb. AGENTS.md requires errors to be
// wrapped with fmt.Errorf("...: %w", err) so callers can errors.Is/As
// through them; a %v or %s on an error flattens it to a string and breaks
// the chain. errorlint checks comparison sites but not the formatting verb,
// so this closes the gap.
func NewWrapErrors() *cop.Func {
	return cop.New(cop.Meta{
		Name:        "Gogo/WrapErrors",
		Description: "fmt.Errorf must wrap error values with %w, not %v/%s (see AGENTS.md)",
		Severity:    cop.Warning,
	}, func(p *cop.Pass) {
		if p.Info == nil {
			return
		}
		p.ForEachCall(func(call *ast.CallExpr) {
			if !cop.IsCallTo(call, "fmt", "Errorf") || len(call.Args) < 2 {
				return
			}
			format, ok := stringLit(call.Args[0])
			if !ok {
				return
			}
			if strings.Contains(format, "%w") {
				return // already wrapping at least one error
			}
			for _, arg := range call.Args[1:] {
				if isErrorType(p.Info.TypeOf(arg)) {
					p.Report(call, "fmt.Errorf interpolates an error without %w — wrap it so errors.Is/As keep working")
					return
				}
			}
		})
	}, cop.WithTypes())
}

// stringLit returns the unquoted value of a string-literal expression.
func stringLit(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	val, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return val, true
}

// isErrorType reports whether t is exactly the built-in error interface.
func isErrorType(t types.Type) bool {
	if t == nil {
		return false
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	return named.Obj() != nil && named.Obj().Name() == "error" && named.Obj().Pkg() == nil
}
