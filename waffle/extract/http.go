package extract

import (
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/specrig/specrig-go/surface"
)

// The path of a request: what is constant stays, what varies is a {} segment —
// an id in the path is data, the rest is the contract.
func (x *extraction) template(p *packages.Package, fd *ast.FuncDecl, e ast.Expr, depth int) ([]string, []string) {
	if s, ok := stringConstant(p.TypesInfo, e); ok {
		return []string{s}, nil
	}
	switch v := e.(type) {
	case *ast.ParenExpr:
		return x.template(p, fd, v.X, depth)
	case *ast.BinaryExpr:
		if v.Op == token.ADD {
			return concat(x.piece(p, fd, v.X, depth), x.piece(p, fd, v.Y, depth)), nil
		}
	case *ast.CallExpr:
		if fn := callee(p.TypesInfo, v); fn != nil && fn.Pkg() != nil && fn.Pkg().Path() == "fmt" && fn.Name() == "Sprintf" && len(v.Args) > 0 {
			if format, ok := stringConstant(p.TypesInfo, v.Args[0]); ok {
				return []string{verbs.ReplaceAllStringFunc(format, func(m string) string {
					if m == "%%" {
						return "%"
					}
					return "{}"
				})}, nil
			}
		}
	}
	return x.follow(p, fd, e, depth, x.template)
}

// "VERB path" for every verb and path a request can carry; a path with no
// constant part is an address from data, somewhere outside.
func (x *extraction) requests(p *packages.Package, fd *ast.FuncDecl, call *ast.CallExpr, s sink) []string {
	paths, lost := x.template(p, fd, call.Args[s.arg], 3)
	if len(lost) > 0 {
		paths = append(paths, external)
	}
	var out []string
	for _, m := range x.verbs(p, fd, call, s) {
		for _, path := range paths {
			path, _, _ = strings.Cut(path, "?")
			out = append(out, strings.ToUpper(m)+" "+path)
		}
	}
	return out
}

// "VERB path" for every verb and path a route can carry, under its mux's prefixes.
func (x *extraction) routes(p *packages.Package, fd *ast.FuncDecl, call *ast.CallExpr, s sink) []string {
	paths, lost := x.resolve(p, fd, call.Args[s.arg], 3)
	x.fail(lost, "routes a path that is not a constant — it is a contract, give it one")
	sel, _ := call.Fun.(*ast.SelectorExpr)
	prefixes, lost := x.prefixes(p, fd, sel.X, 3)
	x.fail(lost, "routes under a mux whose group prefix cannot be told — a field configuration sets goes under flows.prefixes in "+surface.ConfigFile)
	var out []string
	for _, m := range x.verbs(p, fd, call, s) {
		for _, pre := range prefixes {
			for _, path := range paths {
				out = append(out, strings.ToUpper(m)+" "+pre+path)
			}
		}
	}
	return out
}

func (x *extraction) verbs(p *packages.Package, fd *ast.FuncDecl, call *ast.CallExpr, s sink) []string {
	if s.verbArg < 0 {
		return []string{s.verb}
	}
	methods, lost := x.resolve(p, fd, call.Args[s.verbArg], 3)
	x.fail(lost, "names an HTTP method that is not a constant")
	return methods
}

func (x *extraction) fail(lost []string, why string) {
	for _, l := range lost {
		x.lost = append(x.lost, l+": "+why)
	}
}

const (
	external     = "external"
	configPrefix = "{}"
)

var verbs = regexp.MustCompile(`%%|%[-+# 0]*[0-9*]*(\.[0-9*]+)?[a-zA-Z]`)

// Inside a path, a part that cannot be told is a segment's worth of data.
func (x *extraction) piece(p *packages.Package, fd *ast.FuncDecl, e ast.Expr, depth int) []string {
	out, lost := x.template(p, fd, e, depth)
	if len(lost) > 0 || len(out) == 0 {
		return []string{"{}"}
	}
	return out
}

func concat(a, b []string) []string {
	var out []string
	for _, l := range a {
		for _, r := range b {
			out = append(out, l+r)
		}
	}
	return out
}

func (x *extraction) prefixes(p *packages.Package, fd *ast.FuncDecl, e ast.Expr, depth int) ([]string, []string) {
	lost := []string{x.at(e.Pos())}
	switch v := e.(type) {
	case *ast.ParenExpr:
		return x.prefixes(p, fd, v.X, depth)
	case *ast.CallExpr:
		fn := callee(p.TypesInfo, v)
		sel, ok := v.Fun.(*ast.SelectorExpr)
		if fn == nil || !ok || fn.Name() != "Group" || !x.on(fn, httpServer, "Mux") || len(v.Args) == 0 {
			return nil, lost
		}
		outer, l1 := x.prefixes(p, fd, sel.X, depth)
		own, l2 := x.prefix(p, fd, v.Args[0], depth)
		return concat(outer, own), append(l1, l2...)
	case *ast.Ident:
		variable, ok := p.TypesInfo.Uses[v].(*types.Var)
		if !ok || depth == 0 {
			return nil, lost
		}
		fn, _ := p.TypesInfo.Defs[fd.Name].(*types.Func)
		if fn == nil {
			return nil, lost
		}
		params := fn.Type().(*types.Signature).Params()
		for i := range params.Len() {
			if params.At(i) != variable {
				continue
			}
			if x.routerSetup(fn) {
				return []string{""}, nil
			}
			var out, missed []string
			for _, c := range x.callers[fn] {
				outer := enclosing(c.f, c.call)
				if outer == nil || i >= len(c.call.Args) {
					missed = append(missed, x.at(c.call.Pos()))
					continue
				}
				got, l := x.prefixes(c.p, outer, c.call.Args[i], depth-1)
				out = append(out, got...)
				missed = append(missed, l...)
			}
			if out == nil && missed == nil {
				return nil, lost
			}
			return out, missed
		}
		var out, missed []string
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok || len(as.Lhs) != len(as.Rhs) {
				return true
			}
			for i, l := range as.Lhs {
				if lid, ok := l.(*ast.Ident); ok && (p.TypesInfo.Defs[lid] == variable || p.TypesInfo.Uses[lid] == variable) {
					got, lo := x.prefixes(p, fd, as.Rhs[i], depth)
					out = append(out, got...)
					missed = append(missed, lo...)
				}
			}
			return true
		})
		if out == nil && missed == nil {
			return nil, lost
		}
		return out, missed
	}
	return nil, lost
}

func (x *extraction) prefix(p *packages.Package, fd *ast.FuncDecl, e ast.Expr, depth int) ([]string, []string) {
	if s, ok := stringConstant(p.TypesInfo, e); ok {
		return []string{s}, nil
	}
	key, ok := x.fieldKey(p, e)
	if !ok {
		return x.follow(p, fd, e, depth, x.prefix)
	}
	if _, named := x.configured[key]; !named {
		return nil, []string{x.at(e.Pos()) + " (" + key + ")"}
	}
	x.configured[key] = true
	return []string{configPrefix}, nil
}

func (x *extraction) fieldKey(p *packages.Package, e ast.Expr) (string, bool) {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	s := p.TypesInfo.Selections[sel]
	if s == nil || s.Kind() != types.FieldVal {
		return "", false
	}
	named, ok := deref(s.Recv()).(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return "", false
	}
	return strings.TrimPrefix(named.Obj().Pkg().Path(), x.module+"/") + "." + named.Obj().Name() + "." + sel.Sel.Name, true
}

// Setup on a server.Router is where the mux comes from outside — waffle hands it
// the root. Any other function nobody in the module calls is a mux of unknown
// prefix, not the root.
func (x *extraction) routerSetup(fn *types.Func) bool {
	router := x.lib(httpServer, "Router")
	recv := fn.Type().(*types.Signature).Recv()
	if router == nil || recv == nil || fn.Name() != "Setup" {
		return false
	}
	iface, ok := router.Underlying().(*types.Interface)
	return ok && types.Implements(recv.Type(), iface)
}
