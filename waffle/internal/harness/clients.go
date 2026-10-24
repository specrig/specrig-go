package harness

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	waffle "gitlab.com/gorib/waffle/container"
)

type client struct {
	Field   string
	Alias   string
	Path    string
	Service string // RegisterXServer's X; the constructor is NewXClient
}

// A handler registers its service in Start, and protoc-gen-go-grpc puts
// NewXClient beside RegisterXServer. A handler outside the module is not the
// service's contract, and one whose shape is not recognised is reported rather
// than guessed at.
func (s *shape) readClients(module, root string) (missed []string) {
	if s.Grpc.handlers == "" {
		return nil
	}
	body := waffle.InitAppWithBody(s.file)
	constructors := builtBy(body)
	seen := map[string]bool{}
	for _, member := range members(body, s.Grpc.handlers) {
		call, ok := constructors[member]
		if !ok {
			missed = append(missed, member)
			continue
		}
		pkg, fn, ok := strings.Cut(call, ".")
		importPath := s.Imports[pkg]
		if !ok || !strings.HasPrefix(importPath, module+"/") {
			continue
		}
		dir := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(importPath, module+"/")))
		registered, ok := registration(dir, fn)
		if !ok {
			missed = append(missed, member)
			continue
		}
		for _, r := range registered {
			if !seen[r.Path+"."+r.Service] {
				seen[r.Path+"."+r.Service] = true
				s.Clients = append(s.Clients, r)
			}
		}
	}
	name(s.Clients)
	return missed
}

// container.handlers = []T{container.a, container.b}
func members(body *ast.BlockStmt, field string) []string {
	var out []string
	ast.Inspect(body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return true
		}
		if target, ok := waffle.QualifiedBy(assign.Lhs[0], "container"); !ok || target != field {
			return true
		}
		literal, ok := assign.Rhs[0].(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, elt := range literal.Elts {
			if member, ok := waffle.QualifiedBy(elt, "container"); ok {
				out = append(out, member)
			}
		}
		return false
	})
	return out
}

// container.x = s, where s := pkg.NewX(...) earlier in the same block.
func builtBy(body *ast.BlockStmt) map[string]string {
	out := map[string]string{}
	ast.Inspect(body, func(n ast.Node) bool {
		block, ok := n.(*ast.BlockStmt)
		if !ok {
			return true
		}
		locals := map[string]string{}
		for _, stmt := range block.List {
			assign, ok := stmt.(*ast.AssignStmt)
			if !ok || len(assign.Rhs) != 1 {
				continue
			}
			if call, ok := assign.Rhs[0].(*ast.CallExpr); ok && assign.Tok == token.DEFINE {
				if pkg, ok := assign.Lhs[0].(*ast.Ident); ok {
					if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
						if x, ok := selector.X.(*ast.Ident); ok {
							locals[pkg.Name] = x.Name + "." + selector.Sel.Name
						}
					}
				}
				continue
			}
			local, ok := assign.Rhs[0].(*ast.Ident)
			if !ok {
				continue
			}
			for _, lhs := range assign.Lhs {
				if field, ok := waffle.QualifiedBy(lhs, "container"); ok && locals[local.Name] != "" {
					out[field] = locals[local.Name]
				}
			}
		}
		return true
	})
	return out
}

func registration(dir, constructor string) ([]client, bool) {
	files := parseDir(dir)
	var typeName string
	for _, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && fn.Recv == nil && fn.Name.Name == constructor {
				typeName = returned(fn)
			}
		}
	}
	if typeName == "" {
		return nil, false
	}
	var out []client
	for _, file := range files {
		aliases := waffle.ImportAliases(file)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != "Start" || receiver(fn) != typeName || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				x, ok := selector.X.(*ast.Ident)
				if !ok {
					return true
				}
				service, ok := strings.CutPrefix(selector.Sel.Name, "Register")
				service, found := strings.CutSuffix(service, "Server")
				if ok && found && service != "" && aliases[x.Name] != "" {
					out = append(out, client{Path: aliases[x.Name], Service: service})
				}
				return true
			})
		}
	}
	return out, len(out) > 0
}

func parseDir(dir string) []*ast.File {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []*ast.File
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err == nil {
			out = append(out, file)
		}
	}
	return out
}

// The declared result, or failing that the literal returned: a constructor may
// return an interface and build the concrete type inside.
func returned(fn *ast.FuncDecl) string {
	if fn.Type.Results != nil && len(fn.Type.Results.List) > 0 {
		if name := local(fn.Type.Results.List[0].Type); name != "" {
			return name
		}
	}
	var out string
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok || len(ret.Results) == 0 || out != "" {
			return out == ""
		}
		expr := ret.Results[0]
		if unary, ok := expr.(*ast.UnaryExpr); ok {
			expr = unary.X
		}
		if literal, ok := expr.(*ast.CompositeLit); ok {
			out = local(literal.Type)
		}
		return false
	})
	return out
}

func receiver(fn *ast.FuncDecl) string {
	if len(fn.Recv.List) == 0 {
		return ""
	}
	return local(fn.Recv.List[0].Type)
}

func local(expr ast.Expr) string {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if ident, ok := expr.(*ast.Ident); ok && ident.Name != "error" {
		return ident.Name
	}
	return ""
}

// A package the served clients already import keeps their alias.
func (s *shape) nameOutbound() {
	taken := map[string]string{} // alias -> path
	for _, c := range s.Clients {
		taken[c.Alias] = c.Path
	}
	byPath := map[string]string{}
	for alias, p := range taken {
		byPath[p] = alias
	}
	for i, o := range s.Outbound {
		alias, known := byPath[o.Path]
		if !known {
			alias = path.Base(o.Path) + "Proto"
			for n := 2; taken[alias] != ""; n++ {
				alias = path.Base(o.Path) + "Proto" + strconv.Itoa(n)
			}
			taken[alias], byPath[o.Path] = o.Path, alias
		}
		s.Outbound[i].Alias = alias
	}
}

// NotificationService → Notification; two services of one name in different
// packages are both prefixed by their package.
func name(clients []client) {
	short := func(c client) string {
		if trimmed := strings.TrimSuffix(c.Service, "Service"); trimmed != "" {
			return trimmed
		}
		return c.Service
	}
	count := map[string]int{}
	for _, c := range clients {
		count[short(c)]++
	}
	aliases := map[string]string{}
	taken := map[string]bool{}
	for i, c := range clients {
		clients[i].Field = short(c)
		if count[short(c)] > 1 {
			clients[i].Field = exported(path.Base(c.Path)) + short(c)
		}
		alias, known := aliases[c.Path]
		if !known {
			alias = path.Base(c.Path) + "Proto"
			for n := 2; taken[alias]; n++ {
				alias = path.Base(c.Path) + "Proto" + strconv.Itoa(n)
			}
			aliases[c.Path], taken[alias] = alias, true
		}
		clients[i].Alias = alias
	}
	slices.SortFunc(clients, func(a, b client) int { return strings.Compare(a.Field, b.Field) })
}
