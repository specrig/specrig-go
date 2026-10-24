package harness

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"slices"
	"strings"
	"unicode"

	waffle "gitlab.com/gorib/waffle/container"
)

var (
	sqlDb        = waffle.TypeRef{Path: "gitlab.com/gorib/sql", Name: "Db"}
	redisDb      = waffle.TypeRef{Path: "gitlab.com/gorib/redis", Name: "Db"}
	grpcConn     = waffle.TypeRef{Path: "google.golang.org/grpc", Name: "ClientConnInterface"}
	pryLogger    = waffle.TypeRef{Path: "gitlab.com/gorib/pry", Name: "Logger"}
	hiltonTracer = waffle.TypeRef{Path: "gitlab.com/gorib/hilton", Name: "Provider"}
	consumer     = waffle.TypeRef{Path: "gitlab.com/gorib/waffle/tools/scribus", Name: "Consumer"}
	publisher    = waffle.TypeRef{Path: "gitlab.com/gorib/amqp", Name: "Publisher"}
)

// Every list holds Replacements fields: a type the container carries twice is
// filled twice.
type shape struct {
	Postgres     []string
	Redis        []string
	Logger       []string
	Hilton       []string
	Consumer     []string
	Publisher    []string
	Dead         []conn
	Grpc         wiring
	Http         wiring
	Variables    []variable
	Clients      []client
	Outbound     []outbound
	Commands     []string // getters of the commands a step may run itself
	CommandLists []string // getters of the di.Tags collections of commands
	Params       []param
	Imports      map[string]string // alias -> path, as the container file spells them
	file         *ast.File

	paramImports map[string]string // alias -> path the params hook's result types need
}

// Each field is one container getter, not a list of members: the collection is
// a container field of its own, so the harness reads it whole and a tagged
// registration added later reaches the suite without an edit here.
type wiring struct {
	Handlers     string
	Interceptors string
	Stream       string
	Mux          string
	Global       string
	Routers      string
	handlers     string // the unexported field, for its members
}

func (w wiring) Present() bool { return w.Handlers != "" || w.Mux != "" }

type conn struct {
	Field string
	Label string
}

func read(container waffle.Container, importPath string, seams []waffle.Seam) (shape, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, container.File, nil, parser.SkipObjectResolution|parser.ParseComments)
	if err != nil {
		return shape{}, err
	}
	out := shape{file: file, Imports: waffle.ImportAliases(file)}
	collections := collectionFields(file)
	built := constructors(file)
	types := map[string]waffle.TypeRef{}
	for _, seam := range seams {
		if seam.Composite() || collections[seam.Field] {
			continue
		}
		ref := seam.Types[0]
		types[seam.Field] = ref
		switch ref {
		case sqlDb:
			out.Postgres = append(out.Postgres, seam.Field)
		case redisDb:
			out.Redis = append(out.Redis, seam.Field)
		case pryLogger:
			out.Logger = append(out.Logger, seam.Field)
		case hiltonTracer:
			out.Hilton = append(out.Hilton, seam.Field)
		case consumer:
			out.Consumer = append(out.Consumer, seam.Field)
		case publisher:
			out.Publisher = append(out.Publisher, seam.Field)
		case grpcConn:
			out.Dead = append(out.Dead, conn{Field: seam.Field})
		default:
			if built[seam.Field] == publisher.Path+".NewPublisher" {
				out.Publisher = append(out.Publisher, seam.Field)
			}
		}
	}
	out.Outbound = grpcClients(file, types)
	out.Commands, out.CommandLists = commands(file, out.Imports)

	out.Params, out.paramImports, err = parameters(waffle.InitAppWith(file), out.Imports, container.Package, importPath)
	if err != nil {
		return shape{}, fmt.Errorf("%s: %w", container.File, err)
	}

	body := waffle.InitAppWithBody(file)
	graph := readGraph(fset, file, body)
	for i := range out.Dead {
		out.Dead[i].Label = graph.label(out.Dead[i].Field, types)
	}
	slices.SortFunc(out.Dead, func(a, b conn) int { return strings.Compare(a.Field, b.Field) })
	if out.Variables, err = graph.variables(out.filled(), moduleReads(".")); err != nil {
		return shape{}, err
	}

	fields := containerFields(file)
	out.Grpc = commandArgs(body, fields, "NewGrpcCommand")
	out.Http = commandArgs(body, fields, "NewHttpCommand")
	if out.Http.Mux != "" && out.Http.Routers == "" {
		return shape{}, fmt.Errorf("%s passes no []server.Router to NewHttpCommand: the http "+
			"command collects its routers now — register each one with di.Define(NewRouter, "+
			"di.Tag(http.RouterTag)) and regenerate the container", container.File)
	}
	if out.Http.Mux != "" && out.Http.Global == "" {
		return shape{}, fmt.Errorf("%s passes no http.Global to NewHttpCommand: the global "+
			"middleware chain belongs to the server now, not to a router — regenerate the "+
			"container and take the middleware chain out of your routers", container.File)
	}
	return out, nil
}

// What Boot supplies whenever the scenario did not, so the container never
// reaches the constructors under these fields and never reads their variables.
func (s shape) filled() map[string]bool {
	out := map[string]bool{}
	for _, list := range [][]string{s.Postgres, s.Redis, s.Logger, s.Hilton, s.Consumer, s.Publisher} {
		for _, field := range list {
			out[field] = true
		}
	}
	for _, dead := range s.Dead {
		out[dead.Field] = true
	}
	return out
}

// A typed client of another service.
type outbound struct {
	Field string
	Path  string
	Name  string
	Alias string
}

// protoc-gen-go-grpc names XClient's constructor NewXClient.
func grpcClients(file *ast.File, types map[string]waffle.TypeRef) []outbound {
	var out []outbound
	for field, doc := range constructors(file) {
		ref, ok := types[field]
		if ok && strings.HasSuffix(ref.Name, "Client") && doc == ref.Path+".New"+ref.Name {
			out = append(out, outbound{Field: field, Path: ref.Path, Name: ref.Name})
		}
	}
	slices.SortFunc(out, func(a, b outbound) int { return strings.Compare(a.Field, b.Field) })
	return out
}

// What builds each Replacements field, as diogen writes it above the field.
func constructors(file *ast.File) map[string]string {
	out := map[string]string{}
	structure := structType(file, "Replacements")
	if structure == nil {
		return out
	}
	for _, field := range structure.Fields.List {
		if field.Doc == nil {
			continue
		}
		for _, name := range field.Names {
			out[name.Name] = strings.TrimSpace(field.Doc.Text())
		}
	}
	return out
}

func commands(file *ast.File, imports map[string]string) (single, lists []string) {
	structure := structType(file, "Application")
	if structure == nil {
		return nil, nil
	}
	command := func(t ast.Expr) bool {
		selector, ok := t.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Command" {
			return false
		}
		x, ok := selector.X.(*ast.Ident)
		return ok && imports[x.Name] == "gitlab.com/gorib/waffle/app"
	}
	for _, field := range structure.Fields.List {
		into := &single
		if list, ok := field.Type.(*ast.ArrayType); ok && list.Len == nil && command(list.Elt) {
			into = &lists
		} else if !command(field.Type) {
			continue
		}
		for _, name := range field.Names {
			*into = append(*into, exported(name.Name))
		}
	}
	slices.Sort(single)
	slices.Sort(lists)
	return single, lists
}

// A di.Tags collection shares its element type with a single seam, and filling
// a collection with one recorder would be a different container.
func collectionFields(file *ast.File) map[string]bool {
	out := map[string]bool{}
	structure := structType(file, "Replacements")
	if structure == nil {
		return out
	}
	for _, field := range structure.Fields.List {
		if _, ok := field.Type.(*ast.ArrayType); !ok {
			continue
		}
		for _, name := range field.Names {
			out[name.Name] = true
		}
	}
	return out
}

// InitAppWith as a graph: which Replacements field guards each constructor,
// what each constructor reads from the container and from the environment.
type graph struct {
	owner  map[string]string // container field -> Replacements field
	blocks []block
	reads  []envRead
}

type block struct {
	guards []string // Replacements fields
	reads  map[string]bool
	keys   []string
}

func readGraph(fset *token.FileSet, file *ast.File, body *ast.BlockStmt) graph {
	g := graph{owner: map[string]string{}}
	if body != nil {
		stmts, locals := waffle.BuildStatements(body), waffle.Locals(body)
		for _, stmt := range stmts {
			if field, seam, ok := waffle.PrologueAssignment(stmt); ok {
				g.owner[field] = seam
			}
		}
		for _, stmt := range stmts {
			guarded, ok := stmt.(*ast.IfStmt)
			if !ok {
				continue
			}
			guards := g.owners(waffle.GuardedFields(guarded.Cond))
			if len(guards) == 0 {
				continue
			}
			b := block{guards: guards, reads: waffle.ContainerReads(guarded.Body, locals)}
			for _, read := range envReads(fset, file, guarded.Body, nil) {
				b.keys = append(b.keys, read.Key)
			}
			g.blocks = append(g.blocks, b)
		}
	}
	g.reads = envReads(fset, file, file, g.owners)
	return g
}

func (g graph) owners(fields []string) []string {
	var out []string
	for _, field := range fields {
		if seam, ok := g.owner[field]; ok && !slices.Contains(out, seam) {
			out = append(out, seam)
		}
	}
	return out
}

// Who the connection was built for and what it reads: the panic has to say
// which replacement did not take, and nothing here knows a naming convention.
func (g graph) label(field string, types map[string]waffle.TypeRef) string {
	var own []string
	for container, seam := range g.owner {
		if seam == field {
			own = append(own, container)
		}
	}
	var consumers, keys []string
	for _, b := range g.blocks {
		if slices.Contains(b.guards, field) {
			keys = append(keys, b.keys...)
			continue
		}
		if !slices.ContainsFunc(own, func(c string) bool { return b.reads[c] }) {
			continue
		}
		for _, guard := range b.guards {
			if ref, ok := types[guard]; ok {
				consumers = append(consumers, path.Base(ref.Path)+"."+ref.Name)
			}
		}
	}
	slices.Sort(consumers)
	consumers = slices.Compact(consumers)
	slices.Sort(keys)
	keys = slices.Compact(keys)

	label := field
	if len(consumers) > 0 {
		label = strings.Join(consumers, ", ")
	}
	if len(keys) > 0 {
		label += " (" + strings.Join(keys, ", ") + ")"
	}
	return label
}

// Field name to the name of its type, off the Application struct. Arguments to
// the daemon's constructors are bare container fields, so this is what says
// which of them is the interceptor chain and which is the handler list.
func containerFields(file *ast.File) map[string]string {
	out := map[string]string{}
	structure := structType(file, "Application")
	if structure == nil {
		return out
	}
	for _, field := range structure.Fields.List {
		for _, name := range field.Names {
			out[name.Name] = typeName(field.Type)
		}
	}
	return out
}

func structType(file *ast.File, name string) *ast.StructType {
	for _, decl := range file.Decls {
		generic, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range generic.Specs {
			typed, ok := spec.(*ast.TypeSpec)
			if !ok || typed.Name.Name != name {
				continue
			}
			if structure, ok := typed.Type.(*ast.StructType); ok {
				return structure
			}
		}
	}
	return nil
}

// Read off the daemon's own construction call, so the harness serves what the
// daemon serves rather than holding a second opinion about it.
func commandArgs(body *ast.BlockStmt, types map[string]string, constructor string) wiring {
	var out wiring
	if body == nil {
		return out
	}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if name, ok := waffle.QualifiedBy(call.Fun, ""); !ok || name != constructor {
			return true
		}
		for _, arg := range call.Args {
			if name, ok := waffle.QualifiedBy(arg, "container"); ok {
				out.assign(name, types[name])
			}
		}
		return false
	})
	return out
}

// By the field's own type, not by position: every argument here is a bare
// container field, and the constructors take more of them than these.
func (w *wiring) assign(name, kind string) {
	getter := exported(name)
	collection := strings.HasPrefix(kind, "[]")
	elem := strings.TrimPrefix(kind, "[]")
	switch {
	case getter == "" || kind == "":
	case strings.Contains(elem, "StreamInterceptor"):
		w.Stream = getter
	case strings.Contains(elem, "Interceptor"):
		w.Interceptors = getter
	case strings.Contains(elem, "ServiceHandler"):
		w.Handlers, w.handlers = getter, name
	case strings.HasSuffix(elem, "Mux"):
		w.Mux = getter
	// Only as a collection: a single Router left unassigned reaches read(),
	// which refuses it by name rather than scaffolding a broken serve.
	case strings.HasSuffix(elem, "Router") && collection:
		w.Routers = getter
	case strings.HasSuffix(elem, "Global"):
		w.Global = getter
	}
}

func exported(name string) string {
	if name == "" {
		return ""
	}
	return string(unicode.ToUpper(rune(name[0]))) + name[1:]
}

func typeName(expr ast.Expr) string {
	if array, ok := expr.(*ast.ArrayType); ok {
		return "[]" + typeName(array.Elt)
	}
	if selector, ok := expr.(*ast.SelectorExpr); ok {
		return selector.Sel.Name
	}
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}
