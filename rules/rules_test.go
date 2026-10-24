package rules

import (
	"encoding/json"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/specrig/specrig-go/internal/protocol"
	"gopkg.in/yaml.v3"
)

var layout = Layout{
	"generated": {"tests/surface.yaml"},
	"platform":  {"tests/internal/", "specrig.yaml"},
	"cases":     {"tests/feature/**/*.feature"},
	"tests":     {"tests/"},
}

func TestRulesAreTheTableWithTheCasesAndTheLayoutsPatterns(t *testing.T) {
	doc, err := Of(layout, nil)
	if err != nil {
		t.Fatal(err)
	}
	line, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Specrig string           `json:"specrig"`
		Modes   []map[string]any `json:"modes"`
		Classes []map[string]any `json:"classes"`
	}
	if err := json.Unmarshal(line, &got); err != nil {
		t.Fatal(err)
	}
	body, _ := protocol.Files.ReadFile("modes.yaml")
	var table struct {
		Modes []map[string]any `yaml:"modes"`
	}
	if err := yaml.Unmarshal(body, &table); err != nil {
		t.Fatal(err)
	}
	if got.Specrig != "rules/1" || len(got.Modes) != len(table.Modes) {
		t.Fatalf("%s", line)
	}
	for i, m := range table.Modes {
		if keys, want := slices.Sorted(maps.Keys(got.Modes[i])), slices.Sorted(maps.Keys(m)); !slices.Equal(keys, want) || got.Modes[i]["name"] != m["name"] {
			t.Errorf("mode %d has %v, the table %v", i, keys, want)
		}
	}
	if spec := got.Modes[0]; spec["allows"] != "`tests/feature/**/*.feature`" {
		t.Errorf("spec allows %v", spec["allows"])
	}
	want := `[{"name":"humans","patterns":["CONTRIBUTING.md"]},{"name":"generated","patterns":["specrig.md","tests/surface.yaml"]},` +
		`{"name":"platform","patterns":["tests/internal/","specrig.yaml"]},{"name":"cases","patterns":["tests/feature/**/*.feature"]},` +
		`{"name":"tests","patterns":["tests/"]},{"name":"production","patterns":[]}]`
	if classes, _ := json.Marshal(doc.Classes); string(classes) != want {
		t.Errorf("classes %s", classes)
	}
}

func TestALayoutFillingNoClassIsRefused(t *testing.T) {
	if _, err := Of(Layout{"platfrom": {"tests/internal/"}}, nil); err == nil || !strings.Contains(err.Error(), `"platfrom"`) {
		t.Errorf("err = %v", err)
	}
}

func TestTheMarkerReplacesADefaultAreaByItsNameAndAddsItsOwn(t *testing.T) {
	got, err := Areas([]Area{
		{Name: "steps", Paths: []string{"tests/steps/"}, Grant: []string{"platform"}},
		{Name: "gomod", Paths: []string{"go.mod"}, Modes: []string{"platform"}},
	}, []byte("agents:\n  areas:\n    steps:\n      paths: [tests/steps/, tests/support/]\n    events:\n      paths: [../events/]\n      modes: []\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []Area{
		{Name: "events", Paths: []string{"../events/"}, Modes: []string{}},
		{Name: "gomod", Paths: []string{"go.mod"}, Modes: []string{"platform"}},
		{Name: "steps", Paths: []string{"tests/steps/", "tests/support/"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v", got)
	}
}

func TestAnAreaOpenInNoModeSaysSoAndOneTheTableDecidesForSaysNothing(t *testing.T) {
	line, err := json.Marshal([]Area{{Name: "pb", Paths: []string{"api/pb/"}, Modes: []string{}}, {Name: "steps", Paths: []string{"tests/steps/"}}})
	if err != nil {
		t.Fatal(err)
	}
	if want := `[{"name":"pb","paths":["api/pb/"],"modes":[]},{"name":"steps","paths":["tests/steps/"]}]`; string(line) != want {
		t.Errorf("%s", line)
	}
}

func TestAnAreaNamingAModeTheTableDoesNotHaveIsRefusedByItsName(t *testing.T) {
	_, err := Areas(nil, []byte("agents:\n  areas:\n    events:\n      paths: [../events/]\n      grant: [review]\n"))
	if want := `agents.areas.events: unknown mode "review" (have: spec, redgreen, platform, auto)`; err == nil || err.Error() != want {
		t.Errorf("err = %v", err)
	}
}
