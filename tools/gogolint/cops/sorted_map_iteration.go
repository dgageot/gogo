package cops

import (
	"go/ast"
	"go/types"

	"github.com/dgageot/rubocop-go/cop"
)

// NewSortedMapIteration returns a cop that flags ranging over a map while
// appending to a slice. AGENTS.md mandates sorted iteration —
// slices.Sorted(maps.Keys(m)) — whenever map order feeds user-visible or
// determinism-sensitive output, and accumulating map entries into a slice
// is the archetypal way that nondeterminism leaks out. Ranges that only
// write into another map (order-independent) are not flagged, and neither
// are loops in a function that sorts afterwards — range+append followed by
// slices.Sort is an equally deterministic idiom the codebase uses (see
// prefixMatches).
func NewSortedMapIteration() *cop.Func {
	return cop.New(cop.Meta{
		Name:        "Gogo/SortedMapIteration",
		Description: "Iterate maps with slices.Sorted(maps.Keys(m)) when building ordered output (see AGENTS.md)",
		Severity:    cop.Warning,
	}, func(p *cop.Pass) {
		if p.Info == nil {
			return
		}
		p.ForEachFunc(func(fn *ast.FuncDecl) {
			if fn.Body == nil || bodySorts(fn.Body) {
				return
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				rng, ok := n.(*ast.RangeStmt)
				if !ok || rng.X == nil {
					return true
				}
				t := p.Info.TypeOf(rng.X)
				if t == nil {
					return true
				}
				if _, isMap := t.Underlying().(*types.Map); !isMap {
					return true
				}
				if bodyAppends(rng.Body) {
					p.Report(rng, "ranging over a map to build a slice yields nondeterministic order — use slices.Sorted(maps.Keys(m)) or sort the result")
				}
				return true
			})
		})
	}, cop.WithTypes())
}

// bodyAppends reports whether body contains a call to the builtin append,
// the signal that the loop accumulates map entries into a slice. Nested
// range loops are intentionally still visited — an append anywhere in the
// body ties the slice's order to the map's iteration order.
func bodyAppends(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "append" {
			found = true
			return false
		}
		return true
	})
	return found
}

// bodySorts reports whether body contains a slices.Sort* / slices.Sorted or
// sort.* call — the signal that the function establishes a deterministic
// order explicitly rather than relying on map iteration order.
func bodySorts(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		pkg, sel, ok := cop.MatchSelector(call.Fun)
		if !ok {
			return true
		}
		switch pkg {
		case "sort":
			found = true
		case "slices":
			switch sel {
			case "Sort", "SortFunc", "SortStableFunc", "Sorted", "SortedFunc", "SortedStableFunc":
				found = true
			}
		}
		return !found
	})
	return found
}
