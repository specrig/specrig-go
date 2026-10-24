package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/specrig/specrig-go/agents"
	"github.com/specrig/specrig-go/rules"
	"gopkg.in/yaml.v3"
)

var preset = Preset{
	Tool:    "go tool specrig",
	Version: "`example.com/specrig` v0.0.0",
	Layout: rules.Layout{
		"generated": {"tests/surface.yaml"},
		"platform":  {"tests/internal/", "specrig.yaml", "AGENTS.md", "CLAUDE.md"},
		"cases":     {"tests/feature/**/*.feature"},
		"tests":     {"tests/"},
	},
	Areas: func(string) ([]rules.Area, error) {
		return []rules.Area{
			{Name: "gomod", Paths: []string{"go.mod", "go.sum"}, Modes: []string{"platform"}, Grant: []string{"redgreen"}},
			{Name: "steps", Paths: []string{"tests/steps/"}, Grant: []string{"platform"}},
		}, nil
	},
	Slots: map[string]string{
		"what": "service", "cut": "`tests/internal/cut.yaml`", "validate": "`go test ./...`",
		"boundary": "What the harness stands in for is the platform's.", "spec-paths": "`tests/feature/`, `tests/steps/`",
		"platform-paths": "`tests/internal/`, `specrig.yaml`, `go.mod`, `AGENTS.md`", "tasks": "", "generated": "",
		"tool": "specrig", "cases": "`.feature` files in `tests/feature/`", "contracts": "", "describes": "",
		"binding": "Bind at the edge of the service.",
	},
	Own: []string{"## Running", "`go test ./...` runs the suite."},
}

type ran struct {
	code   int
	stdout string
	stderr string
}

func service(t *testing.T, files map[string]string) string {
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func runIn(dir string, p Preset, args ...string) ran {
	var stdout, stderr bytes.Buffer
	code := Run(dir, args, p, &stdout, &stderr)
	return ran{code, stdout.String(), stderr.String()}
}

func reference(t *testing.T) string {
	line, err := agents.Reference("service")
	if err != nil {
		t.Fatal(err)
	}
	return line
}

func TestRulesPrintTheTableTheLayoutAndTheAreasOnOneLine(t *testing.T) {
	got := runIn(service(t, map[string]string{"specrig.yaml": "agents:\n  areas:\n    gen:\n      paths: [gen/]\n      modes: []\n"}), preset, "rules")
	if got.code != 0 || strings.Count(got.stdout, "\n") != 1 {
		t.Fatalf("%+v", got)
	}
	var doc struct {
		Specrig string `json:"specrig"`
		Modes   []struct {
			Name   string `json:"name"`
			Allows string `json:"allows"`
		} `json:"modes"`
		Classes []struct {
			Name     string   `json:"name"`
			Patterns []string `json:"patterns"`
		} `json:"classes"`
		Areas []map[string]any `json:"areas"`
	}
	if err := json.Unmarshal([]byte(got.stdout), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Specrig != "rules/1" || doc.Modes[0].Allows != "`tests/feature/**/*.feature`" || doc.Classes[2].Name != "platform" ||
		strings.Join(doc.Classes[2].Patterns, " ") != "tests/internal/ specrig.yaml AGENTS.md CLAUDE.md" {
		t.Errorf("%s", got.stdout)
	}
	var names []string
	for _, a := range doc.Areas {
		names = append(names, a["name"].(string))
	}
	if strings.Join(names, " ") != "gen gomod steps" {
		t.Errorf("areas %v", names)
	}
}

func TestRulesWithoutAMarkerAreTheDefaults(t *testing.T) {
	got := runIn(t.TempDir(), preset, "rules")
	if got.code != 0 || !strings.Contains(got.stdout, `"areas":[{"name":"gomod","paths":["go.mod","go.sum"],"modes":["platform"],"grant":["redgreen"]},{"name":"steps","paths":["tests/steps/"],"grant":["platform"]}]`) {
		t.Errorf("%+v", got)
	}
}

func TestRulesTakeAndRefuseTheMarkersAsTheProtocolDoes(t *testing.T) {
	files, err := filepath.Glob("../internal/protocol/fixtures/markers/*.yaml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var fixture struct {
			Specrig string `yaml:"specrig"`
			Cases   []struct {
				Name   string `yaml:"name"`
				Marker string `yaml:"marker"`
				Error  string `yaml:"error"`
			} `yaml:"cases"`
		}
		if err := yaml.Unmarshal(body, &fixture); err != nil || fixture.Specrig != "markers-fixtures/1" {
			t.Fatalf("%s: %s, %v", file, fixture.Specrig, err)
		}
		for _, c := range fixture.Cases {
			got := runIn(service(t, map[string]string{"specrig.yaml": c.Marker}), preset, "rules")
			switch {
			case c.Error == "" && got.code != 0:
				t.Errorf("%s: refused: %s", c.Name, got.stderr)
			case c.Error != "" && (got.code != 1 || !strings.HasPrefix(got.stderr, "Error: ") || !strings.Contains(got.stderr, c.Error)):
				t.Errorf("%s: want an error naming %s, got %+v", c.Name, c.Error, got)
			}
		}
	}
}

func TestAMarkerThatIsNotYAMLIsNamed(t *testing.T) {
	got := runIn(service(t, map[string]string{"specrig.yaml": "agents: [\n"}), preset, "rules")
	if got.code != 1 || !strings.HasPrefix(got.stderr, "Error: specrig.yaml: ") {
		t.Errorf("%+v", got)
	}
}

func TestALayoutFillingNoClassIsRefused(t *testing.T) {
	p := preset
	p.Layout = rules.Layout{"platfrom": {"tests/internal/"}}
	if got := runIn(t.TempDir(), p, "rules"); got.code != 1 || !strings.Contains(got.stderr, `"platfrom"`) {
		t.Errorf("%+v", got)
	}
}

func TestAgentsWritesSpecrigMdAndSaysWhetherItChanged(t *testing.T) {
	dir := service(t, map[string]string{"AGENTS.md": "# AGENTS.md\n\n" + reference(t) + "\n"})

	if got := runIn(dir, preset, "agents"); got.code != 0 || got.stdout != "+ specrig.md\n" {
		t.Fatalf("%+v", got)
	}
	written, _ := os.ReadFile(filepath.Join(dir, "specrig.md"))
	for _, want := range []string{
		"# specrig.md\n\n",
		"`go tool specrig agents`",
		"`example.com/specrig` v0.0.0",
		"| spec | `tests/feature/**/*.feature` |",
		"| gomod | `go.mod`, `go.sum` | platform | redgreen |",
		"`tests/internal/`, `specrig.yaml`, `AGENTS.md` and `CLAUDE.md`",
		"## The short version",
		"## Running\n\n`go test ./...` runs the suite.\n",
	} {
		if !strings.Contains(string(written), want) {
			t.Errorf("specrig.md lacks %q", want)
		}
	}
	if strings.Contains(string(written), "{{") {
		t.Error("specrig.md leaves a slot")
	}

	if got := runIn(dir, preset, "agents"); got.code != 0 || got.stdout != "= specrig.md\n" {
		t.Errorf("again: %+v", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "specrig.md"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := runIn(dir, preset, "agents"); got.code != 0 || got.stdout != "~ specrig.md\n" {
		t.Errorf("after an edit: %+v", got)
	}
	if again, _ := os.ReadFile(filepath.Join(dir, "specrig.md")); string(again) != string(written) {
		t.Error("the edit stayed")
	}
}

func TestAgentsFailsWhileAGENTSmdDoesNotBringSpecrigMdInAndWritesItAllTheSame(t *testing.T) {
	dir := service(t, map[string]string{"AGENTS.md": "ours\n"})

	got := runIn(dir, preset, "agents")
	if got.code != 1 || got.stdout != "+ specrig.md\n" ||
		!strings.HasPrefix(got.stderr, "Error: AGENTS.md does not bring specrig.md in: add the line\n\n"+reference(t)+"\n\n") {
		t.Errorf("%+v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "specrig.md")); err != nil {
		t.Error(err)
	}
}

func TestAgentsSaysWhatIsLeftOfAnAGENTSmdThatStillCarriesRule11(t *testing.T) {
	dir := service(t, map[string]string{"AGENTS.md": "# AGENTS.md\n\n" + reference(t) + "\n\n## Rule #11 — specification before code\n"})

	got := runIn(dir, preset, "agents")
	if want := "Error: AGENTS.md still carries Rule #11, which specrig.md holds now: keep the service's own sections in it and delete the rest\n"; got.code != 1 || got.stderr != want {
		t.Errorf("%+v", got)
	}
}

func TestAgentsWritesNothingWhereThereIsNoAGENTSmd(t *testing.T) {
	dir := t.TempDir()

	got := runIn(dir, preset, "agents")
	if got.code != 1 || !strings.HasPrefix(got.stderr, "Error: there is no AGENTS.md here") || !strings.Contains(got.stderr, reference(t)) {
		t.Errorf("%+v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "specrig.md")); !os.IsNotExist(err) {
		t.Errorf("specrig.md: %v", err)
	}
}

func TestAPresetFillingASlotTheCoreFillsIsRefused(t *testing.T) {
	p := preset
	p.Slots = map[string]string{"modes": "| spec | anything |"}
	got := runIn(service(t, map[string]string{"AGENTS.md": reference(t) + "\n"}), p, "agents")
	if got.code != 1 || !strings.Contains(got.stderr, "the preset fills the slot modes") {
		t.Errorf("%+v", got)
	}
}

func TestACommandItDoesNotHaveIsNamed(t *testing.T) {
	if got := runIn(t.TempDir(), preset, "guard"); got.code != 1 || !strings.HasPrefix(got.stderr, "Unknown command \"guard\"\n\n") {
		t.Errorf("%+v", got)
	}
}

func TestCommandsTakeNoArguments(t *testing.T) {
	if got := runIn(t.TempDir(), preset, "rules", "."); got.code != 1 || !strings.HasPrefix(got.stderr, "Error: rules takes no arguments") {
		t.Errorf("%+v", got)
	}
}

func TestInitLaysTheServiceOutThenTheStacksPart(t *testing.T) {
	dir := t.TempDir()
	p := preset
	inits := 0
	p.Init = func() error { inits++; return nil }

	got := runIn(dir, p, "init")
	if want := "+ specrig.yaml\n+ AGENTS.md\n+ specrig.md\n+ CLAUDE.md → AGENTS.md\n"; got.code != 0 || got.stdout != want || inits != 1 {
		t.Fatalf("%+v, the stack's part ran %d times", got, inits)
	}
	if marker, _ := os.ReadFile(filepath.Join(dir, "specrig.yaml")); string(marker) != "guard: [go, tool, specrig]\n" {
		t.Errorf("specrig.yaml: %q", marker)
	}
	stub, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if !strings.HasPrefix(string(stub), "# AGENTS.md\n\nInstructions for AI coding agents working in "+filepath.Base(dir)+".\n\n"+reference(t)+"\n\n## This service\n") {
		t.Errorf("AGENTS.md:\n%s", stub)
	}
	if strings.Contains(string(stub), "../AGENTS.md") {
		t.Error("the stub names a repository's rules there are none of")
	}
	if link, err := os.Readlink(filepath.Join(dir, "CLAUDE.md")); err != nil || link != "AGENTS.md" {
		t.Errorf("CLAUDE.md: %q, %v", link, err)
	}
	if again := runIn(dir, p, "agents"); again.code != 0 {
		t.Errorf("agents after init: %+v", again)
	}

	got = runIn(dir, p, "init")
	if want := "= specrig.yaml — kept\n= AGENTS.md — kept\n= specrig.md\n"; got.code != 0 || got.stdout != want || inits != 2 {
		t.Errorf("again: %+v, the stack's part ran %d times", got, inits)
	}

	p.Init = func() error { return errors.New("no container") }
	if got := runIn(dir, p, "init"); got.code != 1 || got.stderr != "Error: no container\n" {
		t.Errorf("the stack's part failing: %+v", got)
	}
}

func TestInitKeepsWhatTheServiceWroteAndSaysWhatIsMissing(t *testing.T) {
	dir := service(t, map[string]string{
		"specrig.yaml": "agents:\n  areas:\n    gen:\n      paths: [gen/]\n      modes: []\n",
		"AGENTS.md":    "ours",
		"CLAUDE.md":    "theirs\n",
	})

	got := runIn(dir, preset, "init")
	want := "= specrig.yaml — kept; it names no guard: add `guard: [go, tool, specrig]` for the plugin to ask this tool\n" +
		"~ AGENTS.md: the line that brings specrig.md in, at its end\n+ specrig.md\n"
	if got.code != 0 || got.stdout != want {
		t.Errorf("%+v", got)
	}
	if text, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md")); string(text) != "ours\n\n"+reference(t)+"\n" {
		t.Errorf("AGENTS.md: %q", text)
	}
	if text, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md")); string(text) != "theirs\n" {
		t.Errorf("CLAUDE.md: %q", text)
	}

	stale := service(t, map[string]string{"AGENTS.md": "# AGENTS.md\n\n## Rule #11 — specification before code\n"})
	if got := runIn(stale, preset, "init"); !strings.Contains(got.stdout, "~ AGENTS.md: the line that brings specrig.md in, at its end; "+
		"it still carries Rule #11, which specrig.md holds now: keep the service's own sections in it and delete the rest\n") {
		t.Errorf("stale: %+v", got)
	}
}

func TestInitsStubDefersToTheRepositorysAGENTSmd(t *testing.T) {
	root := service(t, map[string]string{"AGENTS.md": "# Repository\n"})
	dir := filepath.Join(root, "billing")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	if got := runIn(dir, preset, "init"); got.code != 0 {
		t.Fatalf("%+v", got)
	}
	stub, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if !strings.Contains(string(stub), "working in billing.") || !strings.Contains(string(stub), "The repository's rules in `../AGENTS.md` hold here too") {
		t.Errorf("AGENTS.md:\n%s", stub)
	}
}

func TestHelpIsTheUsage(t *testing.T) {
	if got := runIn(t.TempDir(), preset, "--help"); got.code != 0 || !strings.HasPrefix(got.stdout, "go tool specrig init\ngo tool specrig agents\ngo tool specrig rules\n") {
		t.Errorf("%+v", got)
	}
}

func TestAnAdaptersOwnCommandRunsWithItsOperandsAndIsInTheUsage(t *testing.T) {
	p := preset
	var got []string
	p.Commands = []Command{{Name: "harness", Operands: "[check]", Description: "writes the harness",
		Run: func(args []string) error { got = args; return nil }}}

	if r := runIn(t.TempDir(), p, "harness", "check"); r.code != 0 || !slices.Equal(got, []string{"check"}) {
		t.Errorf("%+v, ran with %v", r, got)
	}
	if r := runIn(t.TempDir(), p, "--help"); !strings.Contains(r.stdout, "go tool specrig harness [check]\n") ||
		!strings.Contains(r.stdout, "harness: writes the harness") {
		t.Errorf("usage:\n%s", r.stdout)
	}
	p.Commands[0].Run = func([]string) error { return errors.New("no container") }
	if r := runIn(t.TempDir(), p, "harness"); r.code != 1 || r.stderr != "Error: no container\n" {
		t.Errorf("%+v", r)
	}
}

func TestVersionIsTheModulesTheBinaryWasBuiltWith(t *testing.T) {
	if got := Version("gopkg.in/yaml.v3"); got != "v3.0.1" {
		t.Errorf("yaml.v3: %q", got)
	}
	if got := Version("example.com/none"); got != "" {
		t.Errorf("none: %q", got)
	}
}
