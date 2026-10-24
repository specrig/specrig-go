package replacements

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	waffle "gitlab.com/gorib/waffle/container"
)

func Check() error {
	root, err := ModuleRoot()
	if err != nil {
		return err
	}
	containers, err := waffle.DiscoverContainers(root)
	if err != nil {
		return err
	}
	found, err := Inspect(root, containers)
	if err != nil {
		return err
	}
	cfg, err := LoadConfig(root)
	if err != nil {
		return err
	}
	_, err = Verify(root, found, cfg)
	return err
}

type Inspection struct {
	Seams []waffle.Seam
	Paths []string
	Files []string
}

// Takes the containers rather than finding them, because the CLI lets a person
// name one.
func Inspect(root string, containers []waffle.Container) (Inspection, error) {
	module, err := Module(root)
	if err != nil {
		return Inspection{}, err
	}

	var out Inspection
	for _, container := range containers {
		dir, err := filepath.Rel(root, container.Dir)
		if err != nil {
			return Inspection{}, err
		}
		importPath := path.Join(module, filepath.ToSlash(dir))
		seams, err := waffle.ReadSeams(container, importPath)
		if err != nil {
			return Inspection{}, err
		}
		file, err := filepath.Abs(container.File)
		if err != nil {
			return Inspection{}, err
		}
		out.Seams = append(out.Seams, seams...)
		out.Paths = append(out.Paths, importPath)
		out.Files = append(out.Files, file)
	}
	return out, nil
}

type Report struct {
	// Once per field, however many tests fill it in.
	Granted  int
	Warnings []string
}

// The report comes back with the denial too: a warning about the list is worth
// reading most of all when the list has just refused something.
func Verify(root string, found Inspection, cfg Config) (Report, error) {
	report := Report{Warnings: unknownGrants(cfg, found.Seams)}

	filledIn, err := scan(root, found)
	if err != nil {
		return Report{}, err
	}

	byField := fields(found.Seams)
	var refused []violation
	groups := group(filledIn)
	for _, group := range groups {
		if group.Field == positional {
			return report, fmt.Errorf("%s: a Replacements literal without field names — name the fields, "+
				"or nothing can say which seams it replaces", group.Pos[0])
		}
		seam, known := byField[group.Field]
		if !known {
			return report, fmt.Errorf("%s: Replacements has no field %s — the container is out of date, "+
				"and go generate ./... rewrites it", group.Pos[0], group.Field)
		}
		denied := deniedTypes(seam, cfg)
		if len(denied) == 0 {
			continue
		}
		refused = append(refused, violation{Field: group.Field, Types: denied, Where: group.Pos})
	}
	if len(refused) > 0 {
		return report, fmt.Errorf("%s", refusal(refused))
	}
	report.Granted = len(groups)
	return report, nil
}

type filled struct {
	Field string
	Pos   string
}

// An element of the literal with no field name: which seam it fills depends on
// the order of a generated struct, so the check refuses to read it.
const positional = ""

type violation struct {
	Field string
	Types []waffle.TypeRef
	Where []string
}

// Import paths in full: the answer to this message is a line of specrig.yaml, and
// that line is keyed by the whole path.
func refusal(refused []violation) string {
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "%d replaced %s not in replacements.allow:\n", len(refused),
		plural(len(refused), "field is", "fields are"))
	for _, v := range refused {
		names := make([]string, 0, len(v.Types))
		for _, ref := range v.Types {
			names = append(names, ref.String())
		}
		_, _ = fmt.Fprintf(&b, "\n    %s — %s\n", v.Field, strings.Join(names, " + "))
		for _, where := range v.Where {
			_, _ = fmt.Fprintf(&b, "      %s\n", where)
		}
	}
	b.WriteString("\nreplacements.allow in specrig.yaml is what a test may supply itself. It is the\n")
	b.WriteString("platform engineer's decision: report the seam you need instead of granting it.\n")
	return b.String()
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// A composite field replaces every contract it merges, so one ungranted
// constituent refuses the field.
func deniedTypes(seam waffle.Seam, cfg Config) []waffle.TypeRef {
	var out []waffle.TypeRef
	for _, ref := range seam.Types {
		if !cfg.Allowed(ref.Path, ref.Name) {
			out = append(out, ref)
		}
	}
	return out
}

// Two containers can expose the same field name; the union is what has to be
// granted, since either one may be the one under test.
func fields(seams []waffle.Seam) map[string]waffle.Seam {
	out := map[string]waffle.Seam{}
	for _, seam := range seams {
		merged, seen := out[seam.Field]
		if !seen {
			out[seam.Field] = seam
			continue
		}
		// Cloned, because the two seams may share a backing array with the
		// slices ReadSeams still holds.
		types := slices.Clone(merged.Types)
		for _, ref := range seam.Types {
			if !slices.Contains(types, ref) {
				types = append(types, ref)
			}
		}
		merged.Types = types
		out[seam.Field] = merged
	}
	return out
}

// A grant that expired denies nothing, so it is a warning. Worded here, not by
// the caller: the service and the command have to say the same thing.
func unknownGrants(cfg Config, seams []waffle.Seam) []string {
	exposed := map[string]bool{}
	for _, seam := range seams {
		for _, ref := range seam.Types {
			exposed[ref.String()] = true
		}
	}

	var out []string
	for pkg, names := range cfg.Allow {
		for _, name := range names {
			ref := waffle.TypeRef{Path: pkg, Name: name}
			if !exposed[ref.String()] {
				out = append(out, fmt.Sprintf("replacements.allow grants %s, which no container exposes", ref))
			}
		}
	}
	sort.Strings(out)
	return out
}

type occurrences struct {
	Field string
	Pos   []string
}

func group(found []filled) []occurrences {
	byField := map[string][]string{}
	var order []string
	for _, f := range found {
		if _, seen := byField[f.Field]; !seen {
			order = append(order, f.Field)
		}
		byField[f.Field] = append(byField[f.Field], f.Pos)
	}
	sort.Strings(order)

	out := make([]occurrences, 0, len(order))
	for _, field := range order {
		out = append(out, occurrences{Field: field, Pos: byField[field]})
	}
	return out
}

// Types, not syntax: a helper's return, a field of the scenario struct and an
// element of a slice all hold Replacements and none of them resolves by name.
// The whole module, because a replacement is the same decision wherever it is made.
func scan(root string, containers Inspection) ([]filled, error) {
	fset := token.NewFileSet()
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedSyntax | packages.NeedTypes |
			packages.NeedTypesInfo | packages.NeedCompiledGoFiles,
		Tests: true,
		Dir:   root,
		Fset:  fset,
	}, "./...")
	if err != nil {
		return nil, fmt.Errorf("cannot read the module: %w", err)
	}
	// Refusing to answer beats saying "nothing is replaced" about sources nobody
	// read.
	if broken := loadErrors(pkgs); len(broken) > 0 {
		return nil, fmt.Errorf("the module does not type-check, so its tests cannot be read:\n\t%s",
			strings.Join(broken, "\n\t"))
	}

	var out []filled
	read := map[string]bool{}
	for _, pkg := range pkgs {
		for _, file := range pkg.Syntax {
			name := fset.Position(file.Pos()).Filename
			// Tests: true hands the same file back once per test variant.
			if read[name] || excluded(root, name, containers.Files) {
				continue
			}
			read[name] = true
			out = append(out, replaced(file, pkg.TypesInfo, fset, root, containers.Paths)...)
		}
	}
	return out, nil
}

func loadErrors(pkgs []*packages.Package) []string {
	var out []string
	for _, pkg := range pkgs {
		for _, e := range pkg.Errors {
			out = append(out, e.Error())
		}
	}
	slices.Sort(out)
	out = slices.Compact(out)
	// Enough to name the cause; the compiler will list the rest.
	if len(out) > 3 {
		out = out[:3]
	}
	return out
}

// The harness is the platform engineer's own code, and it is where the real
// database is handed in — constraining it with the engineer's own list would
// deny the very thing the list exists to allow.
var harnessDir = filepath.Join("tests", "internal")

func excluded(root, file string, containers []string) bool {
	// The file, not its directory: the generated container assigns every field,
	// but a test beside it is read like any other.
	if slices.Contains(containers, file) {
		return true
	}
	rel, err := filepath.Rel(root, file)
	if err != nil {
		return true
	}
	return rel == harnessDir || strings.HasPrefix(rel, harnessDir+string(filepath.Separator))
}

func replaced(file *ast.File, info *types.Info, fset *token.FileSet, root string, containers []string) []filled {
	var out []filled
	at := func(pos token.Pos) string {
		p := fset.Position(pos)
		rel, err := filepath.Rel(root, p.Filename)
		if err != nil {
			rel = p.Filename
		}
		return fmt.Sprintf("%s:%d", filepath.ToSlash(rel), p.Line)
	}

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CompositeLit:
			if !isReplacements(info.TypeOf(node), containers) {
				return true
			}
			for _, elt := range node.Elts {
				kv, keyed := elt.(*ast.KeyValueExpr)
				if !keyed {
					out = append(out, filled{Field: positional, Pos: at(elt.Pos())})
					continue
				}
				if key, ok := kv.Key.(*ast.Ident); ok {
					out = append(out, filled{Field: key.Name, Pos: at(kv.Pos())})
				}
			}
		case *ast.AssignStmt:
			for _, lhs := range node.Lhs {
				sel, ok := lhs.(*ast.SelectorExpr)
				if !ok || !isReplacements(info.TypeOf(sel.X), containers) {
					continue
				}
				out = append(out, filled{Field: sel.Sel.Name, Pos: at(sel.Pos())})
			}
		}
		return true
	})
	return out
}

const structName = "Replacements"

func isReplacements(t types.Type, containers []string) bool {
	if t == nil {
		return false
	}
	if ptr, ok := types.Unalias(t).(*types.Pointer); ok {
		t = ptr.Elem()
	}
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	if obj.Name() != structName || obj.Pkg() == nil {
		return false
	}
	return slices.Contains(containers, obj.Pkg().Path())
}
