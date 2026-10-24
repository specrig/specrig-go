package extract

import (
	"go/ast"
	"go/types"
	"strings"
)

type kind int

const (
	publishes kind = iota
	consumes
	calls
	serves
	requests // an HTTP call: the path is a template, a variable segment is {}
	routes   // an HTTP route: the path is a constant under the mux's group prefixes
)

func (k kind) String() string {
	return [...]string{"publishes", "consumes", "calls", "serves", "requests", "routes"}[k]
}

const (
	httpServer = "gitlab.com/gorib/http/server"
	httpClient = "gitlab.com/gorib/http/client"
)

// Where a name leaves the service, library by library.
type sink struct {
	pkg, typ, method string
	arg              int
	kind             kind
	verb             string // the HTTP method a call names by itself
	verbArg          int    // or the argument that carries it
}

func at(pkg, typ, method string, arg int, k kind) sink {
	return sink{pkg: pkg, typ: typ, method: method, arg: arg, kind: k, verbArg: -1}
}

func verb(pkg, typ, method string, arg int, k kind) sink {
	s := at(pkg, typ, method, arg, k)
	s.verb = strings.ToUpper(method)
	return s
}

func verbFrom(pkg, typ, method string, verbArg, arg int, k kind) sink {
	s := at(pkg, typ, method, arg, k)
	s.verbArg = verbArg
	return s
}

var sinks = []sink{
	at("gitlab.com/gorib/waffle/tools/outbox", "Publisher", "Publish", 1, publishes),
	at("gitlab.com/gorib/waffle/tools/outbox", "Publisher", "Enqueue", 1, publishes),
	at("gitlab.com/gorib/amqp", "Publisher", "Publish", 1, publishes),
	at("gitlab.com/gorib/amqp", "consumer", "Bind", 0, consumes),
	at("github.com/rabbitmq/amqp091-go", "Channel", "Publish", 1, publishes),
	at("github.com/rabbitmq/amqp091-go", "Channel", "PublishWithContext", 2, publishes),
	at("github.com/rabbitmq/amqp091-go", "Channel", "PublishWithDeferredConfirm", 1, publishes),
	at("github.com/rabbitmq/amqp091-go", "Channel", "PublishWithDeferredConfirmWithContext", 2, publishes),
	at("github.com/rabbitmq/amqp091-go", "Channel", "QueueBind", 1, consumes),
	at("google.golang.org/grpc", "ClientConn", "Invoke", 1, calls),
	at("google.golang.org/grpc", "ClientConn", "NewStream", 2, calls),
	at("google.golang.org/grpc", "ClientConnInterface", "Invoke", 1, calls),
	at("google.golang.org/grpc", "ClientConnInterface", "NewStream", 2, calls),
	at("google.golang.org/grpc", "Server", "RegisterService", 0, serves),
	at("google.golang.org/grpc", "ServiceRegistrar", "RegisterService", 0, serves),
	verb(httpServer, "Mux", "Get", 0, routes),
	verb(httpServer, "Mux", "Post", 0, routes),
	verb(httpServer, "Mux", "Put", 0, routes),
	verb(httpServer, "Mux", "Patch", 0, routes),
	verb(httpServer, "Mux", "Delete", 0, routes),
	verbFrom(httpServer, "Mux", "Handle", 0, 1, routes),
	verb(httpClient, "Client", "Get", 1, requests),
	verb(httpClient, "Client", "Post", 1, requests),
	verb(httpClient, "Client", "Patch", 1, requests),
	verb(httpClient, "Client", "Delete", 1, requests),
	verb(httpClient, "Client", "Head", 1, requests),
	verb(httpClient, "Client", "Options", 1, requests),
	verbFrom(httpClient, "Client", "Do", 1, 2, requests),
	verbFrom(httpClient, "Client", "DoWithoutConcurrent", 1, 2, requests),
}

func (x *extraction) sink(fn *types.Func) (sink, bool) {
	for _, s := range sinks {
		if s.method == fn.Name() && x.on(fn, s.pkg, s.typ) {
			return s, true
		}
	}
	return sink{}, false
}

// A method is the library's when its receiver is the library's type, or an
// interface that type satisfies — the service's own Publisher in front of the
// outbox is the outbox.
func (x *extraction) on(fn *types.Func, pkg, typ string) bool {
	recv := fn.Type().(*types.Signature).Recv()
	if recv == nil {
		return false
	}
	lib := x.lib(pkg, typ)
	if lib == nil {
		return false
	}
	r := deref(recv.Type())
	if named, ok := r.(*types.Named); ok && named.Obj() == lib.Obj() {
		return true
	}
	iface, ok := r.Underlying().(*types.Interface)
	if !ok {
		return false
	}
	var impl types.Type = lib
	if !types.IsInterface(lib) {
		impl = types.NewPointer(lib)
	}
	return types.Implements(impl, iface)
}

func (x *extraction) lib(path, typ string) *types.Named {
	pkg := x.libs[path]
	if pkg == nil {
		return nil
	}
	tn, ok := pkg.Scope().Lookup(typ).(*types.TypeName)
	if !ok {
		return nil
	}
	named, _ := types.Unalias(tn.Type()).(*types.Named)
	return named
}

func deref(t types.Type) types.Type {
	if ptr, ok := types.Unalias(t).(*types.Pointer); ok {
		return types.Unalias(ptr.Elem())
	}
	return types.Unalias(t)
}

// Every package the module reaches, by path: a sink's library is only in play
// if the module imports it somewhere.
func reachable(own []*types.Package) map[string]*types.Package {
	out := map[string]*types.Package{}
	var walk func(*types.Package)
	walk = func(p *types.Package) {
		if out[p.Path()] != nil {
			return
		}
		out[p.Path()] = p
		for _, i := range p.Imports() {
			walk(i)
		}
	}
	for _, p := range own {
		walk(p)
	}
	return out
}

// RegisterService(&pb.<Svc>_ServiceDesc, impl): the descriptor names the service.
func described(info *types.Info, desc ast.Expr) (*types.Package, string, bool) {
	if u, ok := desc.(*ast.UnaryExpr); ok {
		desc = u.X
	}
	var id *ast.Ident
	switch d := desc.(type) {
	case *ast.Ident:
		id = d
	case *ast.SelectorExpr:
		id = d.Sel
	}
	if id == nil {
		return nil, "", false
	}
	v, ok := info.Uses[id].(*types.Var)
	if !ok || v.Pkg() == nil || !strings.HasSuffix(v.Name(), "_ServiceDesc") {
		return nil, "", false
	}
	return v.Pkg(), strings.TrimSuffix(v.Name(), "_ServiceDesc"), true
}
