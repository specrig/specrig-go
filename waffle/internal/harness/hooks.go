package harness

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Found by name alone: the compiler checks the signature against the call the
// generated file makes.
type hooks struct {
	Env       bool // func env(v *Variables)
	Setup     bool // func setup() error
	Resources bool // func resources() []resource.Resource
	Boot      bool // func (a *App) boot(ctx context.Context, cut *<container>.Replacements) error
	Params    bool // func (a *App) params(ctx context.Context) (<InitAppWith's parameters>, err error)
	Extra     bool // type extra struct{...}, embedded in App
}

// Every top-level name the package's own files declare, methods as "Type.Name".
type declared map[string]string // name -> file

func readPackage(dir string) (hooks, declared, error) {
	var h hooks
	names := declared{}
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return h, nil, err
	}
	for _, entry := range entries {
		file := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(file, ".go") || strings.HasSuffix(file, "_test.go") || file == generated {
			continue
		}
		target := filepath.Join(dir, file)
		parsed, err := parser.ParseFile(token.NewFileSet(), target, nil, parser.SkipObjectResolution)
		if err != nil {
			return h, nil, fmt.Errorf("failed to parse %s: %w", target, err)
		}
		for name := range topLevel(parsed) {
			names[name] = target
		}
	}
	_, h.Env = names["env"]
	_, h.Setup = names["setup"]
	_, h.Resources = names["resources"]
	_, h.Boot = names["App.boot"]
	_, h.Params = names["App.params"]
	_, h.Extra = names["extra"]
	return h, names, nil
}

func topLevel(file *ast.File) map[string]bool {
	out := map[string]bool{}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil {
				out[d.Name.Name] = true
				continue
			}
			if recv := receiver(d); recv != "" {
				out[recv+"."+d.Name.Name] = true
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					out[s.Name.Name] = true
				case *ast.ValueSpec:
					for _, name := range s.Names {
						out[name.Name] = true
					}
				}
			}
		}
	}
	return out
}

// A hand-written harness from before the generated one declares the same names,
// and the package would not compile with both.
func collisions(rendered []byte, own declared) error {
	parsed, err := parser.ParseFile(token.NewFileSet(), generated, rendered, parser.SkipObjectResolution)
	if err != nil {
		return err
	}
	byFile := map[string][]string{}
	for name := range topLevel(parsed) {
		if file, taken := own[name]; taken {
			byFile[file] = append(byFile[file], name)
		}
	}
	if len(byFile) == 0 {
		return nil
	}
	var lines []string
	for _, file := range slices.Sorted(maps.Keys(byFile)) {
		slices.Sort(byFile[file])
		lines = append(lines, fmt.Sprintf("%s: %s", file, strings.Join(byFile[file], ", ")))
	}
	return fmt.Errorf("%s declares these now, so the package's own files may not:\n  %s\n"+
		"move what the container cannot say into hooks (env, setup, resources, App.params, App.boot, extra) and delete the rest — "+
		"see github.com/specrig/specrig-go/waffle/docs/harness.md", generated, strings.Join(lines, "\n  "))
}
