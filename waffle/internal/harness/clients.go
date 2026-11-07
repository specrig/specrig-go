package harness

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
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

// The container imports every handler's package, whichever condition registers
// it; one outside the module is not the service's contract.
func (s *shape) readClients(module, root string) (missed []string) {
	if s.Grpc.Handlers == "" {
		return nil
	}
	seen := map[string]bool{}
	for _, importPath := range slices.Compact(slices.Sorted(maps.Values(s.Imports))) {
		rel, ok := strings.CutPrefix(importPath, module+"/")
		if !ok {
			continue
		}
		registered, unregistered := registrations(filepath.Join(root, filepath.FromSlash(rel)))
		for _, handler := range unregistered {
			missed = append(missed, rel+"."+handler)
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

func registrations(dir string) (out []client, unregistered []string) {
	for _, file := range parseDir(dir) {
		aliases := waffle.ImportAliases(file)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !handlerStart(fn, aliases) {
				continue
			}
			before := len(out)
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
			if len(out) == before {
				unregistered = append(unregistered, receiver(fn))
			}
		}
	}
	return out, unregistered
}

func handlerStart(fn *ast.FuncDecl, aliases map[string]string) bool {
	if fn.Recv == nil || fn.Name.Name != "Start" || len(fn.Type.Params.List) != 1 {
		return false
	}
	param := fn.Type.Params.List[0]
	selector, ok := param.Type.(*ast.SelectorExpr)
	if !ok || len(param.Names) > 1 || selector.Sel.Name != "ServiceRegistrar" {
		return false
	}
	x, ok := selector.X.(*ast.Ident)
	return ok && aliases[x.Name] == "google.golang.org/grpc"
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
