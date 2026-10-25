package flows

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestASetComesOutAsTheIndexEveryStackAgreesOn(t *testing.T) {
	dirs, err := filepath.Glob("../internal/protocol/fixtures/flows/*")
	if err != nil || len(dirs) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	for _, dir := range dirs {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			var expect struct {
				Specrig string   `yaml:"specrig"`
				Pages   []string `yaml:"pages"`
				Fails   bool     `yaml:"fails"`
			}
			body, err := os.ReadFile(filepath.Join(dir, "expect.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			if err := yaml.Unmarshal(body, &expect); err != nil || expect.Specrig != "flows-fixtures/1" {
				t.Fatalf("expect.yaml: %v, specrig: %q", err, expect.Specrig)
			}
			out := t.TempDir()
			index := filepath.Join(out, "index.yaml")
			if before, err := os.ReadFile(filepath.Join(dir, "before.yaml")); err == nil {
				writeFile(t, index, string(before))
			}

			err = Run(filepath.Join(dir, "set"), index, io.Discard)
			if failed := err != nil; failed != expect.Fails {
				t.Errorf("the run failed: %v, want %v", err, expect.Fails)
			}
			got, err := os.ReadFile(index)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join(dir, "index.yaml"))
			if err != nil {
				t.Fatalf("index.yaml: %v; the index comes out as\n%s", err, got)
			}
			if !sameDocument(t, string(got), string(want)) {
				t.Errorf("the index comes out as\n%s\nwant\n%s", got, want)
			}
			written, err := filepath.Glob(filepath.Join(out, "*.md"))
			if err != nil {
				t.Fatal(err)
			}
			pages := []string{}
			for _, p := range written {
				pages = append(pages, strings.TrimSuffix(filepath.Base(p), ".md"))
			}
			if slices.Sort(pages); !slices.Equal(pages, expect.Pages) {
				t.Errorf("pages %v, want %v", pages, expect.Pages)
			}
		})
	}
}
