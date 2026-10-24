package agents

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/specrig/specrig-go/rules"
	"gopkg.in/yaml.v3"
)

func TestAGENTSmdIsJudgedAsTheProtocolsFixturesJudgeIt(t *testing.T) {
	files, err := filepath.Glob("../internal/protocol/fixtures/agents/*.yaml")
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
				Name    string  `yaml:"name"`
				Agents  *string `yaml:"agents"`
				Verdict string  `yaml:"verdict"`
			} `yaml:"cases"`
		}
		if err := yaml.Unmarshal(body, &fixture); err != nil || fixture.Specrig != "agents-fixtures/1" {
			t.Fatalf("%s: %s, %v", file, fixture.Specrig, err)
		}
		for _, c := range fixture.Cases {
			if got := Verdict(c.Agents); got != c.Verdict {
				t.Errorf("%s: %s, want %s", c.Name, got, c.Verdict)
			}
		}
	}
}

func filled() map[string]string {
	return map[string]string{
		"what": "service", "writer": "`specrig init`", "version": "`specrig-go` 0.1.0", "cut": "`tests/internal/Harness.cs`", "validate": "`make test`",
		"boundary": "What the harness stands in for is the platform's.", "spec-paths": "`tests/feature/`",
		"platform-paths": "`tests/internal/`", "tasks": "", "modes": ModeRows([]string{"tests/feature/**/*.feature"}),
		"platform-files": "`tests/internal/`", "generated": "", "tool": "specrig", "config": "`specrig.yaml`",
		"areas": "| flows | `tests/flows.yaml` | spec, redgreen, platform | — |",
		"cases": "`.feature` files in `tests/feature/`", "contracts": "", "describes": "", "binding": "Bind at the edge.",
	}
}

func TestEveryPartRendersWithEverySlotFilled(t *testing.T) {
	for _, part := range parts {
		text, err := Render(part, filled())
		if err != nil {
			t.Fatalf("%s: %v", part, err)
		}
		if strings.Contains(text, "{{") || strings.Contains(text, "\n\n\n") {
			t.Errorf("%s:\n%s", part, text)
		}
	}
}

func TestASlotLeftOutIsNamed(t *testing.T) {
	slots := filled()
	delete(slots, "binding")
	if _, err := Render("rule11", slots); err == nil || !strings.Contains(err.Error(), "binding") {
		t.Errorf("err = %v", err)
	}
}

func TestASlotNoPartHasIsNamed(t *testing.T) {
	slots := filled()
	slots["harness"] = "x"
	if _, err := Render("rule11", slots); err == nil || !strings.Contains(err.Error(), "harness") {
		t.Errorf("err = %v", err)
	}
}

func TestAnEmptySlotAloneOnItsLineTakesTheLine(t *testing.T) {
	text, err := Render("rule11", filled())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "contradictory | a test where the defect is, then the fix. None of the cycle below |\n| a **spike**") {
		t.Errorf("the task table kept an empty row:\n%s", text)
	}
}

func TestTheModeTableNamesTheCases(t *testing.T) {
	rows := ModeRows([]string{"tests/feature/**/*.feature"})
	if !strings.Contains(rows, "| spec | `tests/feature/**/*.feature` |") || strings.Count(rows, "\n") != len(rules.Modes())-1 {
		t.Errorf("rows:\n%s", rows)
	}
}

func TestTheAreaTableSaysHowEachAreaOpens(t *testing.T) {
	want := "| gomod | `go.mod` | platform | redgreen |\n" +
		"| steps | `tests/*_test.go` | as the table says | platform |\n" +
		"| pb | `api/pb/` | no mode | — |"
	if got := AreaRows([]rules.Area{
		{Name: "gomod", Paths: []string{"go.mod"}, Modes: []string{"platform"}, Grant: []string{"redgreen"}},
		{Name: "steps", Paths: []string{"tests/*_test.go"}, Grant: []string{"platform"}},
		{Name: "pb", Paths: []string{"api/pb/"}, Modes: []string{}},
	}); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestAnAreaTheOtherDecidesForIsLeftOutOfTheTable(t *testing.T) {
	got := AreaRows([]rules.Area{
		{Name: "deps", Paths: []string{"package.json", "./package-lock.json"}, Modes: []string{"platform"}},
		{Name: "lock", Paths: []string{"package-lock.json"}, Modes: []string{"redgreen"}},
		{Name: "proto", Paths: []string{"package.json"}, Modes: []string{"spec"}},
	})
	if want := "| deps | `package.json`, `./package-lock.json` | platform | — |"; got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestSpecrigMdIsThePartsInTheirOrderAndThenTheStacksOwn(t *testing.T) {
	text, err := SpecrigMd(filled(), []string{"## Running", "`make test` runs the suite."})
	if err != nil {
		t.Fatal(err)
	}
	at := func(s string) int { return strings.Index(text, strings.TrimRight(s, "\n")) }
	var order []int
	for _, part := range []string{"header", "contributing", "short", "rule11"} {
		body, err := Render(part, filled())
		if err != nil {
			t.Fatal(err)
		}
		if part == "short" {
			order = append(order, at("## The short version"))
		}
		order = append(order, at(body))
	}
	order = append(order, at("## Running"))
	if slices.Contains(order, -1) || !slices.IsSorted(order) {
		t.Errorf("the sections are at %v:\n%s", order, text)
	}
	if !strings.HasSuffix(text, "## Running\n\n`make test` runs the suite.\n") || strings.Contains(text, "\n\n\n") {
		t.Errorf("it ends\n%s", text[len(text)-200:])
	}
}

func TestTheReferenceBringsSpecrigMdIn(t *testing.T) {
	line, err := Reference("service")
	if err != nil {
		t.Fatal(err)
	}
	if text := "# AGENTS.md\n\n" + line + "\n"; Verdict(&text) != "ok" || strings.Contains(line, "\n") {
		t.Errorf("%q", line)
	}
}
