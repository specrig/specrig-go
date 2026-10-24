// Package waffle is the adapter for a service built on waffle, the Go framework:
// the layout waffle lays out, its default areas, and the text of specrig.md that is
// waffle's own.
package waffle

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/specrig/specrig-go/cli"
	"github.com/specrig/specrig-go/rules"
	"gopkg.in/yaml.v3"
)

const module = "github.com/specrig/specrig-go/waffle"

var (
	//go:embed text/binding.md
	binding string
	//go:embed text/contracts.md
	contracts string
	//go:embed text/running.md
	running string
)

var layout = rules.Layout{
	"generated": {"tests/surface.yaml", "tests/internal/harness/harness_gen.go", "AGENTS.md", "CLAUDE.md"},
	"platform":  {"waffle.yaml", "specrig.yaml", "tests/internal/"},
	"cases":     {"tests/feature/**/*.feature"},
	"tests":     {"tests/"},
}

// Preset is what cli.Main takes for a waffle service.
func Preset() cli.Preset {
	return cli.Preset{
		Tool:    "go tool specrig",
		Version: "`" + module + "` " + cli.Version(module),
		Layout:  layout,
		Areas:   areas,
		Slots: map[string]string{
			"what":     "service",
			"cut":      "`replacements.allow` in `waffle.yaml`",
			"validate": "the checks Rule #2 in `AGENTS.md` lists",
			"boundary": "`replacements.allow` in `waffle.yaml` is enforced by a test in the ordinary suite, " +
				"not by this rule: with the rule off you may no more grant yourself a seam than with it on.",
			"spec-paths":     "`tests/`, except `tests/internal/`",
			"platform-paths": "`waffle.yaml`, `specrig.yaml`, `tests/internal/`, `go.mod`",
			"tasks":          "",
			"generated": "`tests/surface.yaml` is written by `./waffle surface`, `tests/internal/harness/harness_gen.go` " +
				"by `go generate ./...`, `AGENTS.md` by `./waffle agents`, `specrig.md` by `go tool specrig agents`, " +
				"and the structure of `tests/flows.yaml` by every whole run of the suite, in any mode, and never by hand; " +
				"its `description` fields are yours in any mode.",
			"tool":      "specrig",
			"cases":     "`.feature` files in `tests/feature/`, run by godog",
			"contracts": strings.TrimRight(contracts, "\n"),
			"describes": "",
			"binding":   strings.TrimRight(binding, "\n"),
		},
		Own: []string{strings.TrimRight(running, "\n")},
	}
}

// proto follows waffle.yaml's proto_dir, so in a monorepo it lands on the shared
// contracts by itself.
func areas(dir string) ([]rules.Area, error) {
	protoDir := "api/proto"
	body, err := os.ReadFile(filepath.Join(dir, "waffle.yaml"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	var config struct {
		ProtoDir string `yaml:"proto_dir"`
	}
	if err := yaml.Unmarshal(body, &config); err != nil {
		return nil, fmt.Errorf("waffle.yaml: %w", err)
	}
	if config.ProtoDir != "" {
		protoDir = config.ProtoDir
	}
	return []rules.Area{
		{Name: "flows", Paths: []string{"tests/flows.yaml"}, Modes: []string{"spec", "redgreen", "platform"}},
		{Name: "gomod", Paths: []string{"go.mod", "go.sum"}, Modes: []string{"platform"}, Grant: []string{"redgreen"}},
		{Name: "proto", Paths: []string{strings.TrimSuffix(protoDir, "/") + "/"}, Modes: []string{"spec", "platform"}},
		{Name: "steps", Paths: []string{"tests/*_test.go"}, Grant: []string{"platform"}},
	}, nil
}
