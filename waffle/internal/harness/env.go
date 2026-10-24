package harness

import (
	"bytes"
	"cmp"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"strings"

	waffle "gitlab.com/gorib/waffle/container"
)

// One variable, as a field of the generated Variables. A read with no fallback
// has no value of its own, so the harness always sets one; a variable only read
// with a fallback is set only when the env hook changed it, because the fallback
// is the container's own answer and writing it back does not always round-trip —
// an empty []string{} comes back from the environment as [""].
type variable struct {
	Key     string
	Type    string
	Default string // Go source; empty for the zero value
	List    bool
	Always  bool
	imports map[string]string // alias -> path the type and default need
}

type envRead struct {
	variable
	guards []string // Replacements fields whose constructor makes the read
	at     token.Position
}

var (
	requiredReaders = map[string]bool{
		"NeedValue": true, "NeedArray": true, "NeedJson": true,
		"MustValue": true, "MustArray": true, "MustJson": true,
	}
	fallbackReaders = map[string]bool{"Value": true, "Array": true, "Json": true}
	durationUnits   = map[string]bool{"Nanosecond": true, "Microsecond": true, "Millisecond": true, "Second": true, "Minute": true, "Hour": true}
)

// Walked with the guards that enclose each read, so a read under a filled seam
// is known to be skipped. owners is nil when only the keys are wanted.
func envReads(fset *token.FileSet, file *ast.File, root ast.Node, owners func([]string) []string) []envRead {
	imports := waffle.ImportAliases(file)
	var out []envRead
	var walk func(n ast.Node, guards []string)
	walk = func(n ast.Node, guards []string) {
		ast.Inspect(n, func(node ast.Node) bool {
			if node == nil {
				return false
			}
			if guarded, ok := node.(*ast.IfStmt); ok && node != n && owners != nil {
				if fields := waffle.GuardedFields(guarded.Cond); len(fields) > 0 {
					if guarded.Init != nil {
						walk(guarded.Init, guards)
					}
					walk(guarded.Body, append(slices.Clone(guards), owners(fields)...))
					if guarded.Else != nil {
						walk(guarded.Else, guards)
					}
					return false
				}
			}
			if read, ok := waffle.EnvReadOf(fset, node, imports); ok {
				if typed, ok := readEnv(read); ok {
					typed.guards = guards
					out = append(out, typed)
				}
			}
			return true
		})
	}
	walk(root, nil)
	return out
}

func moduleReads(root string) []envRead {
	var out []envRead
	for _, read := range waffle.EnvReads(root) {
		if typed, ok := readEnv(read); ok {
			out = append(out, typed)
		}
	}
	return out
}

func readEnv(read waffle.EnvRead) (envRead, bool) {
	// Custom is the field for what has no field of its own.
	if !token.IsIdentifier(read.Key) || read.Key == "Custom" {
		return envRead{}, false
	}
	name, imports := read.Reader, read.Imports
	v := variable{Key: read.Key, List: strings.HasSuffix(name, "Array"), Always: requiredReaders[name], imports: map[string]string{}}

	switch {
	case requiredReaders[name] && strings.HasSuffix(name, "Json"):
		v.Type, v.Default = "string", `"null"`
	case name == "Json":
		// The raw JSON; empty is not JSON, so the container keeps its fallback.
		v.Type = "string"
	case requiredReaders[name]:
		elem, ok := v.source(read.Type, imports)
		if !ok {
			return envRead{}, false
		}
		v.Type, v.Default = elem, placeholder(read.Key, elem)
		if v.List {
			v.Type = "[]" + elem
			v.Default = v.Type + "{" + v.Default + "}"
		}
	case fallbackReaders[name] && len(read.Call.Args) == 2:
		fallback := read.Call.Args[1]
		if !v.contained(fallback, imports) {
			return envRead{}, false
		}
		typ, ok := v.source(read.Type, imports)
		if !ok {
			typ, ok = v.infer(fallback, imports)
		}
		if !ok {
			return envRead{}, false
		}
		v.Type, v.Default = typ, render(fallback)
		if v.List && !strings.HasPrefix(typ, "[]") {
			v.Type = "[]" + typ
		}
	default:
		return envRead{}, false
	}
	return envRead{variable: v, at: read.Pos}, true
}

// The type as source, with whatever package it names imported.
func (v *variable) source(expr ast.Expr, aliases map[string]string) (string, bool) {
	if expr == nil || !v.contained(expr, aliases) {
		return "", false
	}
	return render(expr), true
}

// What a fallback's type is, when the call leaves it to inference.
func (v *variable) infer(expr ast.Expr, aliases map[string]string) (string, bool) {
	if composite, ok := expr.(*ast.CompositeLit); ok && composite.Type != nil {
		return v.source(composite.Type, aliases)
	}
	typed, untyped := "", ""
	ast.Inspect(expr, func(n ast.Node) bool {
		if typed != "" {
			return false
		}
		switch t := n.(type) {
		case *ast.CallExpr:
			typed, _ = v.source(t.Fun, aliases)
			return false
		case *ast.SelectorExpr:
			if x, ok := t.X.(*ast.Ident); ok && aliases[x.Name] == "time" && durationUnits[t.Sel.Name] {
				typed = x.Name + ".Duration"
			}
			return false
		case *ast.BasicLit:
			if untyped == "" {
				untyped = map[token.Token]string{token.STRING: "string", token.INT: "int", token.FLOAT: "float64", token.CHAR: "rune"}[t.Kind]
			}
		case *ast.Ident:
			if untyped == "" && (t.Name == "true" || t.Name == "false") {
				untyped = "bool"
			}
		}
		return true
	})
	out := cmp.Or(typed, untyped)
	return out, out != ""
}

// Only literals, operators and other packages' exported names: anything that
// names a declaration of its own file cannot be copied out of it.
func (v *variable) contained(expr ast.Expr, aliases map[string]string) bool {
	ok := true
	ast.Inspect(expr, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.SelectorExpr:
			x, isIdent := t.X.(*ast.Ident)
			if !isIdent || aliases[x.Name] == "" {
				ok = false
				return false
			}
			v.imports[x.Name] = aliases[x.Name]
			return false
		case *ast.Ident:
			if !predeclared[t.Name] {
				ok = false
			}
		case *ast.FuncLit, *ast.CallExpr:
			if call, isCall := t.(*ast.CallExpr); isCall && len(call.Args) == 1 {
				return true // a conversion
			}
			ok = false
		}
		return ok
	})
	return ok
}

var predeclared = map[string]bool{
	"true": true, "false": true, "nil": true,
	"string": true, "bool": true, "rune": true, "byte": true,
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"float32": true, "float64": true,
}

// A string names its own key, so a call that reaches it says which variable it
// came from. Everything else is its zero value, which parses as its type.
func placeholder(key, typ string) string {
	switch typ {
	case "string":
		return strconv.Quote(strings.ToLower(strings.ReplaceAll(key, "_", "-")) + ".harness.invalid")
	case "bool":
		return "false"
	}
	return "0"
}

func render(expr ast.Expr) string {
	var out bytes.Buffer
	if err := format.Node(&out, token.NewFileSet(), expr); err != nil {
		return ""
	}
	return out.String()
}

// Two files may import one package under different aliases.
func (v variable) qualified() string {
	expr, err := parser.ParseExpr(v.Type)
	if err != nil {
		return v.Type
	}
	ast.Inspect(expr, func(n ast.Node) bool {
		if selector, ok := n.(*ast.SelectorExpr); ok {
			if x, ok := selector.X.(*ast.Ident); ok && v.imports[x.Name] != "" {
				x.Name = v.imports[x.Name]
			}
		}
		return true
	})
	return render(expr)
}

// A key the container reads only under seams Boot fills stays out wherever else
// it is spelled: config/ holds the markup's copy of the container's own reads.
func (g graph) variables(filled map[string]bool, module []envRead) ([]variable, error) {
	made, live := map[string]bool{}, map[string]bool{}
	var reads []envRead
	for _, read := range g.reads {
		made[read.Key] = true
		if !slices.ContainsFunc(read.guards, func(seam string) bool { return filled[seam] }) {
			live[read.Key] = true
			reads = append(reads, read)
		}
	}
	for _, read := range module {
		if !made[read.Key] || live[read.Key] {
			reads = append(reads, read)
		}
	}

	byKey := map[string]envRead{}
	for _, read := range reads {
		kept, seen := byKey[read.Key]
		if seen && kept.qualified() != read.qualified() {
			return nil, fmt.Errorf("%s is read as %s at %s and as %s at %s: Variables has one field for it, so the reads have to agree on its type",
				read.Key, kept.Type, kept.at, read.Type, read.at)
		}
		if !seen || read.Always && !kept.Always {
			byKey[read.Key] = read
		}
	}
	out := make([]variable, 0, len(byKey))
	for _, read := range byKey {
		out = append(out, read.variable)
	}
	slices.SortFunc(out, func(a, b variable) int { return strings.Compare(a.Key, b.Key) })
	return out, nil
}
