package extract

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/specrig/specrig-go/surface"
	"github.com/specrig/specrig-go/waffle/internal/gomod"
)

// Extract reads the module's own code — not its tests — for what crosses its
// edge.
func Extract(root string) (surface.Surface, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return surface.Surface{}, err
	}
	module, err := gomod.Path(root)
	if err != nil {
		return surface.Surface{}, err
	}
	cfg, err := surface.LoadConfig(root)
	if err != nil {
		return surface.Surface{}, err
	}
	fset := token.NewFileSet()
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
		Dir:  root,
		Fset: fset,
	}, "./...")
	if err != nil {
		return surface.Surface{}, fmt.Errorf("cannot read the module: %w", err)
	}
	var own []*packages.Package
	for _, p := range pkgs {
		for _, e := range p.Errors {
			return surface.Surface{}, fmt.Errorf("the module does not type-check: %v", e)
		}
		if len(p.GoFiles) == 0 || suite(root, p.GoFiles[0]) {
			continue
		}
		own = append(own, p)
	}

	roots := make([]*types.Package, 0, len(own))
	for _, p := range own {
		roots = append(roots, p.Types)
	}
	x := &extraction{root: root, module: module, fset: fset, callers: map[*types.Func][]site{}, libs: reachable(roots), configured: map[string]bool{}}
	for _, key := range cfg.Prefixes {
		x.configured[key] = false
	}
	for _, p := range own {
		for _, f := range p.Syntax {
			ast.Inspect(f, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					if fn := callee(p.TypesInfo, call); fn != nil {
						x.callers[fn] = append(x.callers[fn], site{p, f, call})
					}
				}
				return true
			})
		}
	}
	out := surface.Surface{Specrig: surface.Format, Service: service(module)}
	for _, p := range own {
		for _, f := range p.Syntax {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				if consumer(p.TypesInfo, fd) {
					keys := constants(p.TypesInfo, fd.Body)
					if len(keys) == 0 {
						x.lost = append(x.lost, x.at(fd.Pos())+": Keys returns no constant")
					}
					out.Consumes = append(out.Consumes, keys...)
					continue
				}
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					fn := callee(p.TypesInfo, call)
					if fn == nil {
						return true
					}
					if s, ok := x.sink(fn); ok && s.arg < len(call.Args) {
						if s.kind == serves && len(call.Args) == 2 {
							pkg, svc, ok := described(p.TypesInfo, call.Args[0])
							if !ok {
								x.lost = append(x.lost, x.at(call.Pos())+": serves a descriptor that is not a generated <Svc>_ServiceDesc")
							}
							out.Serves = append(out.Serves, implementing(p.TypesInfo.TypeOf(call.Args[1]), pkg, svc)...)
							return true
						}
						if s.kind == requests {
							out.Calls = append(out.Calls, x.requests(p, fd, call, s)...)
							return true
						}
						if s.kind == routes {
							out.Serves = append(out.Serves, x.routes(p, fd, call, s)...)
							return true
						}
						names, lost := x.resolve(p, fd, call.Args[s.arg], 3)
						for _, l := range lost {
							x.lost = append(x.lost, fmt.Sprintf("%s: %s a name that is not a constant — it is a contract, give it one", l, s.kind))
						}
						switch s.kind {
						case publishes:
							out.Publishes = append(out.Publishes, names...)
						case consumes:
							out.Consumes = append(out.Consumes, names...)
						case calls:
							out.Calls = append(out.Calls, names...)
						}
					}
					if m := rpc(fn); m != "" {
						out.Calls = append(out.Calls, m)
					}
					out.Serves = append(out.Serves, registered(p.TypesInfo, fn, call)...)
					return true
				})
			}
		}
	}
	for key, read := range x.configured {
		if !read {
			x.lost = append(x.lost, surface.ConfigFile+": flows.prefixes names "+key+", which no group prefix reads")
		}
	}
	if len(x.lost) > 0 {
		slices.Sort(x.lost)
		return surface.Surface{}, fmt.Errorf("%s", strings.Join(slices.Compact(x.lost), "\n"))
	}
	out.Domains, err = domains(root, module, own)
	if err != nil {
		return surface.Surface{}, err
	}
	for _, list := range []*[]string{&out.Publishes, &out.Consumes, &out.Serves, &out.Calls, &out.Domains} {
		slices.Sort(*list)
		*list = slices.Compact(*list)
	}
	return out, nil
}

// The component suite is the service's tests, not the service.
func suite(root, file string) bool {
	rel, err := filepath.Rel(root, file)
	return err != nil || rel == "tests" || strings.HasPrefix(rel, "tests"+string(filepath.Separator))
}

type extraction struct {
	root       string
	module     string
	fset       *token.FileSet
	callers    map[*types.Func][]site
	libs       map[string]*types.Package
	configured map[string]bool // flows.prefixes, each true once a group prefix reads it
	lost       []string
}

type site struct {
	p    *packages.Package
	f    *ast.File
	call *ast.CallExpr
}

func (x *extraction) at(pos token.Pos) string {
	p := x.fset.Position(pos)
	rel, err := filepath.Rel(x.root, p.Filename)
	if err != nil {
		rel = p.Filename
	}
	return fmt.Sprintf("%s:%d", filepath.ToSlash(rel), p.Line)
}

func callee(info *types.Info, call *ast.CallExpr) *types.Func {
	var id *ast.Ident
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		id = fun
	case *ast.SelectorExpr:
		id = fun.Sel
	}
	if id == nil {
		return nil
	}
	fn, _ := info.Uses[id].(*types.Func)
	return fn
}

// tools/scribus's handlers name their keys in Keys() []string.
func consumer(info *types.Info, fd *ast.FuncDecl) bool {
	if fd.Recv == nil || fd.Name.Name != "Keys" {
		return false
	}
	fn, ok := info.Defs[fd.Name].(*types.Func)
	if !ok {
		return false
	}
	sig := fn.Type().(*types.Signature)
	if sig.Params().Len() != 0 || sig.Results().Len() != 1 {
		return false
	}
	slice, ok := sig.Results().At(0).Type().(*types.Slice)
	if !ok {
		return false
	}
	elem, ok := slice.Elem().(*types.Basic)
	return ok && elem.Kind() == types.String
}

func (x *extraction) resolve(p *packages.Package, fd *ast.FuncDecl, arg ast.Expr, depth int) ([]string, []string) {
	if s, ok := stringConstant(p.TypesInfo, arg); ok {
		return []string{s}, nil
	}
	return x.follow(p, fd, arg, depth, x.resolve)
}

type evaluate func(p *packages.Package, fd *ast.FuncDecl, e ast.Expr, depth int) ([]string, []string)

// Through a local to its assignments, through a parameter to its callers; what
// arrives is read by eval, the rule of whoever asked.
func (x *extraction) follow(p *packages.Package, fd *ast.FuncDecl, arg ast.Expr, depth int, eval evaluate) ([]string, []string) {
	lost := []string{x.at(arg.Pos())}
	for {
		conv, ok := arg.(*ast.CallExpr)
		if !ok || len(conv.Args) != 1 {
			break
		}
		if tv, ok := p.TypesInfo.Types[conv.Fun]; !ok || !tv.IsType() {
			break
		}
		arg = conv.Args[0]
	}
	id, ok := arg.(*ast.Ident)
	if !ok || depth == 0 {
		return nil, lost
	}
	v, ok := p.TypesInfo.Uses[id].(*types.Var)
	if !ok {
		return nil, lost
	}
	fn, _ := p.TypesInfo.Defs[fd.Name].(*types.Func)
	if fn == nil {
		return nil, lost
	}
	params := fn.Type().(*types.Signature).Params()
	index := -1
	for i := range params.Len() {
		if params.At(i) == v {
			index = i
		}
	}
	if index < 0 {
		var out []string
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok || len(as.Lhs) != len(as.Rhs) {
				return true
			}
			for i, l := range as.Lhs {
				if lid, ok := l.(*ast.Ident); ok && (p.TypesInfo.Defs[lid] == v || p.TypesInfo.Uses[lid] == v) {
					// lo.Ternary(cond, A, B) and its kind: every constant is a value it may hold.
					got, lost := eval(p, fd, as.Rhs[i], depth)
					if len(lost) > 0 || len(got) == 0 {
						got = constants(p.TypesInfo, as.Rhs[i])
					}
					out = append(out, got...)
				}
			}
			return true
		})
		if out == nil {
			return nil, lost
		}
		return out, nil
	}
	var out, missed []string
	for _, c := range x.callers[fn] {
		if index >= len(c.call.Args) {
			continue
		}
		outer := enclosing(c.f, c.call)
		if outer == nil {
			missed = append(missed, x.at(c.call.Pos()))
			continue
		}
		keys, l := eval(c.p, outer, c.call.Args[index], depth-1)
		out = append(out, keys...)
		missed = append(missed, l...)
	}
	if out == nil && missed == nil {
		return nil, lost
	}
	return out, missed
}

func enclosing(f *ast.File, n ast.Node) *ast.FuncDecl {
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil && fd.Pos() <= n.Pos() && n.End() <= fd.End() {
			return fd
		}
	}
	return nil
}

func stringConstant(info *types.Info, e ast.Expr) (string, bool) {
	tv, ok := info.Types[e]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(tv.Value), true
}

func constants(info *types.Info, body ast.Node) []string {
	var out []string
	ast.Inspect(body, func(n ast.Node) bool {
		e, ok := n.(ast.Expr)
		if !ok {
			return true
		}
		if s, ok := stringConstant(info, e); ok {
			out = append(out, s)
			return false
		}
		return true
	})
	return out
}

// protoc-gen-go-grpc names every method of a client <Svc>Client in a constant
// <Svc>_<Method>_FullMethodName beside it.
func rpc(fn *types.Func) string {
	recv := fn.Type().(*types.Signature).Recv()
	if recv == nil {
		return ""
	}
	named, ok := types.Unalias(recv.Type()).(*types.Named)
	if !ok || !strings.HasSuffix(named.Obj().Name(), "Client") {
		return ""
	}
	return fullMethod(named.Obj().Pkg(), strings.TrimSuffix(named.Obj().Name(), "Client"), fn.Name())
}

func fullMethod(pkg *types.Package, svc, method string) string {
	if pkg == nil {
		return ""
	}
	c, ok := pkg.Scope().Lookup(svc + "_" + method + "_FullMethodName").(*types.Const)
	if !ok {
		return ""
	}
	return constant.StringVal(c.Val())
}

func registered(info *types.Info, fn *types.Func, call *ast.CallExpr) []string {
	name := fn.Name()
	if !strings.HasPrefix(name, "Register") || !strings.HasSuffix(name, "Server") || len(call.Args) != 2 {
		return nil
	}
	svc := strings.TrimSuffix(strings.TrimPrefix(name, "Register"), "Server")
	return implementing(info.TypeOf(call.Args[1]), fn.Pkg(), svc)
}

func implementing(t types.Type, pkg *types.Package, svc string) []string {
	if t == nil || pkg == nil {
		return nil
	}
	ms := types.NewMethodSet(t)
	if _, isPtr := t.(*types.Pointer); !isPtr {
		ms = types.NewMethodSet(types.NewPointer(t))
	}
	var out []string
	for i := range ms.Len() {
		m, ok := ms.At(i).Obj().(*types.Func)
		if !ok || !implemented(m) {
			continue
		}
		if full := fullMethod(pkg, svc, m.Name()); full != "" {
			out = append(out, full)
		}
	}
	return out
}

func implemented(m *types.Func) bool {
	recv := m.Type().(*types.Signature).Recv().Type()
	if ptr, ok := recv.(*types.Pointer); ok {
		recv = ptr.Elem()
	}
	if types.IsInterface(recv) {
		return false
	}
	named, ok := types.Unalias(recv).(*types.Named)
	return !ok || !strings.HasPrefix(named.Obj().Name(), "Unimplemented")
}

func service(module string) string { return path.Base(module) }
