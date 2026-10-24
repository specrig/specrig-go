package harness

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/types"
	"maps"
	"slices"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
)

// One parameter of InitAppWith, in the order it takes them. The context is
// Boot's own, the Replacements is the cut, the rest come from the params hook.
type param struct {
	Name        string // as the container declares it
	Type        string // as the harness spells it
	Context     bool
	Replacement bool
	Local       string // in Boot
}

// Markup-function parameters, which diogen puts before the Replacements.
func parameters(fn *ast.FuncDecl, aliases map[string]string, pkg, importPath string) ([]param, map[string]string, error) {
	if fn == nil {
		return nil, nil, errors.New("no InitAppWith — regenerate the container")
	}
	var out []param
	imports := map[string]string{}
	for _, field := range fn.Type.Params.List {
		for _, name := range field.Names {
			p := param{Name: name.Name}
			switch {
			case isIdent(field.Type, "Replacements"):
				p.Replacement, p.Local = true, "cut"
			case isContext(field.Type, aliases):
				p.Context, p.Local = true, "ctx"
			default:
				typ, err := qualify(field.Type, aliases, pkg, importPath, imports)
				if err != nil {
					return nil, nil, fmt.Errorf("InitAppWith parameter %s: %w", name.Name, err)
				}
				p.Type = typ
			}
			out = append(out, p)
		}
	}
	if !slices.ContainsFunc(out, func(p param) bool { return p.Replacement }) {
		return nil, nil, errors.New("InitAppWith takes no Replacements — regenerate the container")
	}
	return out, imports, nil
}

// A parameter named like a local of Boot or a package it imports would shadow it.
func (s *shape) nameLocals(taken map[string]bool) {
	for i, p := range s.Params {
		if p.Local != "" {
			continue
		}
		local := p.Name
		for taken[local] {
			local += "Param"
		}
		s.Params[i].Local = local
	}
}

func (s shape) hooked() []param {
	var out []param
	for _, p := range s.Params {
		if !p.Context && !p.Replacement {
			out = append(out, p)
		}
	}
	return out
}

func locals(params []param) string {
	names := make([]string, 0, len(params))
	for _, p := range params {
		names = append(names, p.Local)
	}
	return strings.Join(names, ", ")
}

// The hook to write, with the imports its types need, for the error that asks for it.
func (s shape) paramsHook() string {
	results := make([]string, 0, len(s.hooked())+1)
	for _, p := range s.hooked() {
		results = append(results, p.Name+" "+p.Type)
	}
	results = append(results, "err error")
	hook := fmt.Sprintf("func (a *App) params(ctx context.Context) (%s)", strings.Join(results, ", "))
	var imports []string
	for _, alias := range slices.Sorted(maps.Keys(s.paramImports)) {
		imports = append(imports, fmt.Sprintf("%s %q", alias, s.paramImports[alias]))
	}
	if len(imports) == 0 {
		return hook
	}
	return hook + "\nimporting " + strings.Join(imports, ", ")
}

func isIdent(expr ast.Expr, name string) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == name
}

func isContext(expr ast.Expr, aliases map[string]string) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Context" {
		return false
	}
	x, ok := selector.X.(*ast.Ident)
	return ok && aliases[x.Name] == "context"
}

// The type as the harness package spells it: a name of the container's own
// package is qualified by it, another package's keeps the container's alias.
func qualify(expr ast.Expr, aliases map[string]string, pkg, importPath string, imports map[string]string) (string, error) {
	clone, err := parser.ParseExpr(render(expr))
	if err != nil {
		return "", err
	}
	var failed error
	qualified := astutil.Apply(clone, func(c *astutil.Cursor) bool {
		switch n := c.Node().(type) {
		case *ast.SelectorExpr:
			if x, ok := n.X.(*ast.Ident); ok {
				p, known := aliases[x.Name]
				if !known {
					failed = fmt.Errorf("%q is not an import of the container", x.Name)
					return false
				}
				imports[x.Name] = p
			}
			return false
		case *ast.Ident:
			// The field names of a func or struct type are not types.
			if c.Name() != "Names" && types.Universe.Lookup(n.Name) == nil {
				c.Replace(&ast.SelectorExpr{X: ast.NewIdent(pkg), Sel: ast.NewIdent(n.Name)})
				imports[pkg] = importPath
			}
		}
		return true
	}, nil)
	if failed != nil {
		return "", failed
	}
	return render(qualified.(ast.Expr)), nil
}
