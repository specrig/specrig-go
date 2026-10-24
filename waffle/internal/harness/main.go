package harness

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"go/format"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/specrig/specrig-go/waffle/internal/command"
	waffle "gitlab.com/gorib/waffle/container"
)

//go:embed templates/*
var templateFiles embed.FS

var templates = command.NewTemplates(templateFiles)

const generated = "harness_gen.go"

var harnessDir = filepath.Join("tests", "internal", "harness")

func New() *cmd {
	return &cmd{command.New("harness",
		"Regenerate the component harness from the container; the first run also scaffolds the suite")}
}

type cmd struct {
	*command.Command
}

// Run from the module root or, through the go:generate line in harness.go, from
// the harness package: every path below is the root's.
func (c *cmd) Run(_ context.Context, args ...string) error {
	if err := c.Operands(args, 0, 0); err != nil {
		return err
	}
	p, err := command.OpenProject()
	if err != nil {
		return err
	}
	if err := os.Chdir(p.Root()); err != nil {
		return err
	}

	own, names, err := readPackage(harnessDir)
	if err != nil {
		return err
	}
	params, found, err := plan(p, own)
	if err != nil {
		return err
	}

	// The package's own file is what marks a harness as started, so a scaffold
	// somebody deleted on purpose is not written back on every generate.
	if _, err := os.Stat(filepath.Join(harnessDir, "harness.go")); errors.Is(err, os.ErrNotExist) {
		if err := p.Write(templates, scaffold(params, found), params); err != nil {
			return err
		}
		command.ReportWarning("the suite imports what go.mod may not require yet: run go mod tidy")
	}
	// After the scaffold: the hook goes into harness.go, and a harness.go that
	// exists is what stops the suite from being written.
	if len(found.hooked()) > 0 && !own.Params {
		return fmt.Errorf("the container takes parameters only the caller has, and Boot asks a hook for them: "+
			"declare in %s\n\t%s\nBoot passes its own ctx as the context — see github.com/specrig/specrig-go/waffle/docs/harness.md",
			filepath.Join(harnessDir, "harness.go"), strings.ReplaceAll(found.paramsHook(), "\n", "\n\t"))
	}
	if len(found.hooked()) == 0 && own.Params {
		command.ReportWarning("App.params is declared, but the container takes no parameter it would supply: delete it")
	}

	rendered, err := templates.Render(tpl(generated+".tpl"), params)
	if err != nil {
		return err
	}
	formatted, err := format.Source(rendered)
	if err != nil {
		return fmt.Errorf("the generated %s does not parse: %w", generated, err)
	}
	if err := collisions(formatted, names); err != nil {
		return err
	}
	for _, member := range found.missed {
		command.ReportWarning("no Register…Server call found for the handler " + member + ": write its client by hand")
	}
	target := filepath.Join(harnessDir, generated)
	if changed, err := command.WriteIfChanged(p.Path(target), formatted); err != nil || !changed {
		return err
	}
	command.ReportAction("generated " + target)
	return nil
}

type found struct {
	shape
	missed []string
}

func scaffold(params map[string]any, found found) map[string]string {
	container := params["ContainerDir"].(string)
	sources := map[string]string{
		tpl("harness.go.tpl"):           filepath.Join(harnessDir, "harness.go"),
		tpl("suite_test.go.tpl"):        filepath.Join("tests", "suite_test.go"),
		tpl("boot_test.go.tpl"):         filepath.Join("tests", "boot_test.go"),
		tpl("replacements_test.go.tpl"): filepath.Join(container, "replacements_test.go"),
	}
	if len(found.Postgres) > 0 {
		sources[tpl("migrations_test.go.tpl")] = filepath.Join("tests", "migrations_test.go")
	}
	return sources
}

func plan(project *command.Project, own hooks) (map[string]any, found, error) {
	module := project.Module()
	containers, err := waffle.DiscoverContainers(".")
	if err != nil {
		return nil, found{}, err
	}
	container := containers[0]
	importPath := path.Join(module, filepath.ToSlash(container.Dir))

	seams, err := waffle.ReadSeams(container, importPath)
	if err != nil {
		return nil, found{}, err
	}
	s, err := read(container, importPath, seams)
	if err != nil {
		return nil, found{}, err
	}
	missed := s.readClients(module, project.Root())
	s.nameOutbound()

	service := path.Base(module)
	migrations := migrations(module)
	godog := usesGodog()
	imported := imports(s, own, migrations, importPath, container.Package, godog)
	s.nameLocals(bootLocals(imported))
	p := map[string]any{
		"Module":       module,
		"Service":      service,
		"EnvPrefix":    strings.ToUpper(strings.ReplaceAll(service, "-", "_")),
		"Container":    importPath,
		"ContainerDir": container.Dir,
		"Package":      container.Package,
		"Imports":      imported,
		"Hooked":       locals(s.hooked()),
		"InitArgs":     locals(s.Params),
		"Resources":    registry(s),
		"Migrations":   migrations,
		"Postgres":     s.Postgres,
		"Redis":        s.Redis,
		"Logger":       s.Logger,
		"Hilton":       s.Hilton,
		"Consumer":     s.Consumer,
		"Publisher":    s.Publisher,
		"Dead":         records(s.Dead, func(c conn) map[string]string { return map[string]string{"Field": c.Field, "Label": c.Label} }),
		"Variables":    records(s.Variables, variableRecord),
		"Fallbacks":    slices.ContainsFunc(s.Variables, func(v variable) bool { return !v.Always }),
		"Clients": records(s.Clients, func(c client) map[string]string {
			return map[string]string{"Field": c.Field, "Alias": c.Alias, "Service": c.Service}
		}),
		"Outbound": records(s.Outbound, func(o outbound) map[string]string {
			return map[string]string{"Field": o.Field, "Alias": o.Alias, "Name": o.Name}
		}),
		"Godog":            godog,
		"Commands":         s.Commands,
		"CommandLists":     s.CommandLists,
		"Grpc":             s.Grpc.Handlers != "",
		"GrpcHandlers":     s.Grpc.Handlers,
		"GrpcInterceptors": s.Grpc.Interceptors,
		"GrpcStream":       s.Grpc.Stream,
		"Http":             s.Http.Mux != "",
		"HttpMux":          s.Http.Mux,
		"HttpGlobal":       s.Http.Global,
		"HttpRouters":      s.Http.Routers,
		"HookEnv":          own.Env,
		"HookSetup":        own.Setup,
		"HookResources":    own.Resources,
		"HookBoot":         own.Boot,
		"HookExtra":        own.Extra,
	}
	return p, found{shape: s, missed: missed}, nil
}

func variableRecord(v variable) map[string]string {
	flag := func(b bool) string {
		if b {
			return "true"
		}
		return ""
	}
	return map[string]string{"Key": v.Key, "Type": v.Type, "Default": v.Default, "List": flag(v.List), "Always": flag(v.Always)}
}

// Records as maps, the shape every command hands its templates.
func records[T any](items []T, record func(T) map[string]string) []map[string]string {
	out := make([]map[string]string, 0, len(items))
	for _, item := range items {
		out = append(out, record(item))
	}
	return out
}

func tpl(name string) string { return filepath.Join("templates", name) }

// A suite not on godog is not made to depend on it.
func usesGodog() bool {
	body, err := os.ReadFile("go.mod")
	return err == nil && strings.Contains(string(body), "github.com/cucumber/godog ")
}

// Standard library first, then the rest in path order, the way goimports groups.
func imports(s shape, own hooks, migrations []map[string]string, container, pkg string, godog bool) []map[string]string {
	std := []string{"context", "fmt", "os", "testing"}
	var rest []map[string]string
	add := func(alias, p string) { rest = append(rest, map[string]string{"Alias": alias, "Path": p}) }

	lists := slices.ContainsFunc(s.Variables, func(v variable) bool { return v.List })
	if lists {
		std = append(std, "strings")
	}
	if slices.ContainsFunc(s.Variables, func(v variable) bool { return v.List && !v.Always }) {
		std = append(std, "slices")
	}
	// What a variable's type or fallback names, under the alias its source used.
	aliases := map[string]string{}
	for _, v := range s.Variables {
		for alias, p := range v.imports {
			aliases[alias] = p
		}
	}
	for _, alias := range slices.Sorted(maps.Keys(aliases)) {
		p := aliases[alias]
		switch {
		case slices.Contains(std, p):
		case !strings.Contains(strings.Split(p, "/")[0], ".") && alias == path.Base(p):
			std = append(std, p)
		default:
			add(alias, p)
		}
	}
	add("", "github.com/specrig/specrig-go/resource")
	add("", "github.com/specrig/specrig-go/waffle/trace")
	add("", "github.com/specrig/specrig-go/flows")
	if len(s.Commands)+len(s.CommandLists) > 0 {
		add("waffleApp", "gitlab.com/gorib/waffle/app")
	}
	if godog {
		add("", "github.com/cucumber/godog")
	}
	add(pkg, container)
	if len(s.Postgres) > 0 {
		std = append(std, "io/fs")
		add("", "gitlab.com/gorib/sql/sqltest")
		add("waffleMigrate", "gitlab.com/gorib/waffle/tools/migrate")
		for _, m := range migrations {
			add(m["Alias"], m["Path"])
		}
	}
	if len(s.Redis) > 0 {
		add("", "gitlab.com/gorib/redis/redistest")
	}
	if len(s.Logger) > 0 {
		add("", "gitlab.com/gorib/pry")
	}
	if len(s.Hilton) > 0 {
		add("", "gitlab.com/gorib/hilton")
	}
	if len(s.Consumer) > 0 {
		add("", "gitlab.com/gorib/waffle/tools/scribus")
	}
	if len(s.Publisher) > 0 {
		add("", "gitlab.com/gorib/amqp")
	}
	if len(s.Dead) > 0 {
		add("grpcClient", "gitlab.com/gorib/grpc/client")
	}
	if s.Grpc.Present() {
		add("waffleGrpc", "gitlab.com/gorib/waffle/tools/grpc")
	}
	if s.Http.Present() {
		add("waffleHttp", "gitlab.com/gorib/waffle/tools/http")
	}
	seen := map[string]bool{}
	for _, c := range s.Clients {
		if !seen[c.Path] {
			seen[c.Path] = true
			add(c.Alias, c.Path)
		}
	}
	for _, o := range s.Outbound {
		if !seen[o.Path] {
			seen[o.Path] = true
			add(o.Alias, o.Path)
		}
	}

	slices.Sort(std)
	slices.SortFunc(rest, func(a, b map[string]string) int { return strings.Compare(a["Path"], b["Path"]) })
	out := make([]map[string]string, 0, len(std)+len(rest)+1)
	for _, p := range std {
		out = append(out, map[string]string{"Alias": "", "Path": p})
	}
	out = append(out, map[string]string{"Alias": "", "Path": ""}) // the blank line between the groups
	return append(out, rest...)
}

func importName(alias, p string) string {
	if alias != "" {
		return alias
	}
	return path.Base(p)
}

// What a parameter's local in Boot must not shadow.
func bootLocals(imported []map[string]string) map[string]bool {
	out := map[string]bool{"ctx": true, "cut": true, "app": true, "err": true, "application": true,
		"logger": true, "telemetry": true, "consumer": true, "publisher": true}
	for _, imp := range imported {
		if imp["Path"] != "" {
			out[importName(imp["Alias"], imp["Path"])] = true
		}
	}
	return out
}

// The literal resource.New is given, in the order the systems are declared
// above it. Empty for a service that reaches no infrastructure at all, and Up,
// Reset and Down then do nothing.
func registry(s shape) []string {
	var out []string
	if len(s.Postgres) > 0 {
		out = append(out, "postgres")
	}
	if len(s.Redis) > 0 {
		out = append(out, "cache")
	}
	return out
}

// The migration packages a domain gets from make:domain:migrate, discovered
// rather than configured: the directory is where the command puts it. In name
// order, because no domain's migrations may depend on another's —
// CheckMigrations is what holds that.
func migrations(module string) []map[string]string {
	entries, err := os.ReadDir("domain")
	if err != nil {
		return nil
	}
	var out []map[string]string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join("domain", entry.Name(), "cmd", "migrate")
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		out = append(out, map[string]string{
			"Name":  entry.Name(),
			"Alias": entry.Name() + "Migrate",
			"Path":  path.Join(module, filepath.ToSlash(dir)),
		})
	}
	slices.SortFunc(out, func(a, b map[string]string) int { return strings.Compare(a["Name"], b["Name"]) })
	return out
}
