package extract

import (
	"go/ast"
	"go/types"
	"path"
	"strings"

	"golang.org/x/tools/go/packages"

	"gitlab.com/gorib/waffle/boundaries"
)

// The container says which pair; the adapter's calls through an interface B's
// service implements say which methods.
func domains(root, module string, own []*packages.Package) ([]string, error) {
	crossings, err := boundaries.Inspect(root)
	if err != nil {
		return nil, err
	}
	wired := map[[2]string]bool{}
	for _, c := range crossings {
		if c.Rule != boundaries.Wiring || !c.Legal {
			continue
		}
		from, _, ok1 := layer(c.From)
		to, _, ok2 := layer(c.To)
		if ok1 && ok2 {
			wired[[2]string{from, to}] = true
		}
	}
	if len(wired) == 0 {
		return nil, nil
	}

	services := map[string][]*types.TypeName{}
	for _, p := range own {
		d, l, ok := layer(strings.TrimPrefix(p.PkgPath, module+"/"))
		if !ok || l != "service" {
			continue
		}
		for _, name := range p.Types.Scope().Names() {
			if tn, ok := p.Types.Scope().Lookup(name).(*types.TypeName); ok && !types.IsInterface(tn.Type()) {
				services[d] = append(services[d], tn)
			}
		}
	}

	var out []string
	for _, p := range own {
		from, l, ok := layer(strings.TrimPrefix(p.PkgPath, module+"/"))
		if !ok || l != "adapter" {
			continue
		}
		for _, f := range p.Syntax {
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				fn := callee(p.TypesInfo, call)
				if fn == nil {
					return true
				}
				recv := fn.Type().(*types.Signature).Recv()
				if recv == nil {
					return true
				}
				iface, ok := recv.Type().Underlying().(*types.Interface)
				if !ok {
					return true
				}
				for to, candidates := range services {
					if !wired[[2]string{from, to}] {
						continue
					}
					for _, tn := range candidates {
						if implements(tn, iface) {
							out = append(out, from+" → "+to+": "+tn.Name()+"."+fn.Name())
						}
					}
				}
				return true
			})
		}
	}
	return out, nil
}

func implements(tn *types.TypeName, iface *types.Interface) bool {
	return types.Implements(types.NewPointer(tn.Type()), iface) || types.Implements(tn.Type(), iface)
}

// domain/<name>/<layer>/… — the domain and its layer.
func layer(rel string) (string, string, bool) {
	parts := strings.Split(path.Clean(rel), "/")
	if len(parts) < 3 || parts[0] != "domain" {
		return "", "", false
	}
	return parts[1], parts[2], true
}
