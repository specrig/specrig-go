package waffle

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specrig/specrig-go/agents"
	"github.com/specrig/specrig-go/cli"
)

func service(t *testing.T, files map[string]string) string {
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func run(t *testing.T, dir string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli.Run(dir, args, Preset(), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func areasOf(t *testing.T, dir string) map[string][]string {
	t.Helper()
	code, out, errs := run(t, dir, "rules")
	if code != 0 {
		t.Fatalf("rules: %s", errs)
	}
	var doc struct {
		Areas []struct {
			Name  string   `json:"name"`
			Paths []string `json:"paths"`
		} `json:"areas"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	byName := map[string][]string{}
	for _, a := range doc.Areas {
		byName[a.Name] = a.Paths
	}
	return byName
}

func TestSpecrigMdHasEverySlotFilledAndNamesNoMakeTarget(t *testing.T) {
	reference, err := agents.Reference("service")
	if err != nil {
		t.Fatal(err)
	}
	dir := service(t, map[string]string{"AGENTS.md": "# AGENTS.md\n\n" + reference + "\n"})

	if code, out, errs := run(t, dir, "agents"); code != 0 || out != "+ specrig.md\n" {
		t.Fatalf("%d %q %s", code, out, errs)
	}
	written, _ := os.ReadFile(filepath.Join(dir, "specrig.md"))
	for _, want := range []string{
		"`go tool specrig agents` writes this file with `github.com/specrig/specrig-go/waffle` ",
		"with `replacements.allow` in `waffle.yaml` read before any of them",
		"| proto | `api/proto/` | spec, platform | — |",
		"| steps | `tests/*_test.go` | as the table says | platform |",
		"`waffle.yaml`, `specrig.yaml` and `tests/internal/`",
		"#### When the cut does not work, stop",
		"## Running\n",
	} {
		if !strings.Contains(string(written), want) {
			t.Errorf("specrig.md lacks %q", want)
		}
	}
	for _, unwanted := range []string{"{{", "`make "} {
		if strings.Contains(string(written), unwanted) {
			t.Errorf("specrig.md holds %q", unwanted)
		}
	}
}

func TestProtoFollowsWaffleYamlsProtoDir(t *testing.T) {
	if got := areasOf(t, service(t, map[string]string{"waffle.yaml": "proto_dir: ../../shared/api/proto\n"}))["proto"]; strings.Join(got, " ") != "../../shared/api/proto/" {
		t.Errorf("with proto_dir: %v", got)
	}
	if got := areasOf(t, t.TempDir())["proto"]; strings.Join(got, " ") != "api/proto/" {
		t.Errorf("without waffle.yaml: %v", got)
	}
}

func TestAWaffleYamlThatIsNotYAMLIsNamed(t *testing.T) {
	if code, _, errs := run(t, service(t, map[string]string{"waffle.yaml": "proto_dir: [\n"}), "rules"); code != 1 || !strings.HasPrefix(errs, "Error: waffle.yaml: ") {
		t.Errorf("%d %s", code, errs)
	}
}
