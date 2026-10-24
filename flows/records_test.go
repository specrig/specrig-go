package flows

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/specrig/specrig-go/trace"
)

func TestATraceComesOutAsTheRecordEveryWriterAgreesOn(t *testing.T) {
	files, err := filepath.Glob("../internal/protocol/fixtures/records/*.trace.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".trace.json")
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var runs []struct {
			Case    string `json:"case"`
			Records []struct {
				Seq     int64           `json:"seq"`
				Dir     string          `json:"dir"`
				Kind    string          `json:"kind"`
				Name    string          `json:"name"`
				Step    string          `json:"step"`
				Request json.RawMessage `json:"request"`
				Error   string          `json:"error"`
			} `json:"records"`
		}
		if err := json.Unmarshal(body, &runs); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		cases := features{dir: home, comments: map[caseKey]string{}, files: map[string][]string{}}
		var records []trace.Record
		for _, run := range runs {
			feature, title, _ := strings.Cut(run.Case, ": ")
			cases.comments[caseKey{feature, title}] = ""
			cases.files[title] = append(cases.files[title], feature)
			for _, r := range run.Records {
				records = append(records, trace.Record{Seq: r.Seq, Scenario: title, Feature: feature, Step: r.Step,
					Dir: r.Dir, Kind: r.Kind, Name: r.Name, Request: r.Request, Error: r.Error})
			}
		}
		got := header + string(encode(build(name, records, cases)))

		want, err := os.ReadFile(strings.TrimSuffix(file, ".trace.json") + ".flows.yaml")
		if err != nil {
			t.Fatalf("%s.flows.yaml: %v; the record comes out as\n%s", name, err, got)
		}
		if !sameDocument(t, got, string(want)) {
			t.Errorf("%s comes out as\n%s\nwant\n%s", name, got, want)
		}
	}
}

// The header word for word, then the same document in the same order; a scalar
// may be quoted either way, which the protocol leaves to each writer's YAML.
func sameDocument(t *testing.T, got, want string) bool {
	t.Helper()
	gotHead, gotBody, _ := strings.Cut(got, "\nspecrig:")
	wantHead, wantBody, _ := strings.Cut(want, "\nspecrig:")
	var a, b yaml.Node
	if err := yaml.Unmarshal([]byte("specrig:"+gotBody), &a); err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal([]byte("specrig:"+wantBody), &b); err != nil {
		t.Fatal(err)
	}
	return gotHead == wantHead && sameNode(&a, &b)
}

func sameNode(a, b *yaml.Node) bool {
	if a.Kind != b.Kind || a.Tag != b.Tag || a.Value != b.Value || len(a.Content) != len(b.Content) {
		return false
	}
	for i := range a.Content {
		if !sameNode(a.Content[i], b.Content[i]) {
			return false
		}
	}
	return true
}
