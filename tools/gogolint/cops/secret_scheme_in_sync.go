package cops

import (
	"go/ast"
	"strings"

	"github.com/dgageot/rubocop-go/cop"
)

// NewSecretSchemeInSync returns a cop that enforces the "keep in sync"
// contract secrets.go documents in its own comments: every secret-scheme
// constant (named secretScheme*) must be listed in the supportedSecretSchemes
// slice AND dispatched on in resolveSecretURI via strings.HasPrefix. Adding a
// backend constant but forgetting either site silently breaks validation or
// resolution, so this turns the prose convention into an enforced one.
func NewSecretSchemeInSync() *cop.Func {
	return cop.New(cop.Meta{
		Name:        "Gogo/SecretSchemeInSync",
		Description: "secretScheme* consts must appear in supportedSecretSchemes and resolveSecretURI",
		Severity:    cop.Warning,
	}, func(p *cop.Pass) {
		schemes := schemeConstNames(p.File)
		if len(schemes) == 0 {
			return
		}
		listed := identsInSliceLiteral(p.File, "supportedSecretSchemes")
		dispatched := identArgsOf(p.File, "resolveSecretURI", "strings", "HasPrefix")

		var anchor ast.Node = p.File.Name
		if fn := p.FuncDecl("resolveSecretURI"); fn != nil {
			anchor = fn.Name
		}
		for _, name := range schemes {
			if !listed[name] {
				p.Reportf(anchor, "secret scheme %q is not listed in supportedSecretSchemes", name)
			}
			if !dispatched[name] {
				p.Reportf(anchor, "secret scheme %q is not dispatched in resolveSecretURI (strings.HasPrefix)", name)
			}
		}
	}, cop.WithScope(cop.OnlyFile("taskfile/secrets.go")))
}

// schemeConstNames returns the names of all top-level string consts whose
// name begins with "secretScheme".
func schemeConstNames(file *ast.File) []string {
	var names []string
	for name := range cop.StringConstsIn(file, func(n string) bool {
		return strings.HasPrefix(n, "secretScheme")
	}) {
		names = append(names, name)
	}
	return names
}

// identsInSliceLiteral returns the set of bare identifiers used as elements
// of the composite literal assigned to the named top-level var, e.g. the
// elements of `var supportedSecretSchemes = []string{secretSchemeOp}`.
func identsInSliceLiteral(file *ast.File, varName string) map[string]bool {
	out := map[string]bool{}
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, n := range vs.Names {
				if n.Name != varName || i >= len(vs.Values) {
					continue
				}
				cl, ok := vs.Values[i].(*ast.CompositeLit)
				if !ok {
					continue
				}
				for _, elt := range cl.Elts {
					if id, ok := elt.(*ast.Ident); ok {
						out[id.Name] = true
					}
				}
			}
		}
	}
	return out
}

// identArgsOf returns the set of bare identifiers passed as any argument to
// pkg.sel(...) calls that appear inside the named top-level function. Used to
// collect the scheme idents referenced by strings.HasPrefix in resolveSecretURI.
func identArgsOf(file *ast.File, funcName, pkg, sel string) map[string]bool {
	out := map[string]bool{}
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Recv != nil || fd.Name == nil || fd.Name.Name != funcName {
			continue
		}
		cop.ForEachCallIn(fd, func(call *ast.CallExpr) {
			if !cop.IsCallTo(call, pkg, sel) {
				return
			}
			for _, arg := range call.Args {
				if id, ok := arg.(*ast.Ident); ok {
					out[id.Name] = true
				}
			}
		})
	}
	return out
}
