package flows

import (
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestProseComesBackWithTheAnchorARunLost(t *testing.T) {
	in := Input{Message: "command rent:close", Branches: []Branch{{Cases: []string{"jobs.feature: a"}}}}
	described := in
	described.Description = "Closes a contract"

	lost := reconcile(Doc{Inputs: []Input{described}}, Doc{})
	if want := []Orphan{{Anchor: in.Message, Description: "Closes a contract"}}; !reflect.DeepEqual(lost.Orphaned, want) {
		t.Fatalf("after the run that lost it: %+v, want %+v", lost.Orphaned, want)
	}
	back := reconcile(lost, Doc{Inputs: []Input{in}})
	if back.Inputs[0].Description != "Closes a contract" || back.Orphaned != nil {
		t.Errorf("after the run that brought it back: %+v", back)
	}
}

const (
	shop  = "specrig: flows/1\nservice: shop\ninputs:\n  - message: http POST /orders\n    branches:\n      - sends: [grpc /stock.S/Reserve]\n        cases: [a]\n"
	stock = "specrig: flows/1\nservice: stock\ninputs:\n  - message: grpc /stock.S/Reserve\n    branches:\n      - cases: [b]\n"
)

func TestAFlowsProseComesBackWithTheFlow(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "shop", "tests", "flows.yaml"), shop)
	writeFile(t, filepath.Join(root, "stock", "tests", "flows.yaml"), stock)
	index := filepath.Join(root, "docs", "flows.yaml")
	writeFile(t, index, "orphaned:\n  - anchor: flow shop-http-post-orders\n    title: Placing an order\n    description: Reserves the stock\n")

	files, _, err := Assemble(root, index)
	if err != nil {
		t.Fatal(err)
	}
	var got Index
	if err := yaml.Unmarshal(files[index], &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Flows) != 1 || got.Flows[0].Title != "Placing an order" || got.Flows[0].Description != "Reserves the stock" || got.Orphaned != nil {
		t.Errorf("got flows %+v, orphaned %+v", got.Flows, got.Orphaned)
	}
}

func TestAServiceIsFoundByItsRecordAndAFileOnlyAtItsPathIsNamed(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "shop", "tests", "flows.yaml"), shop)
	writeFile(t, filepath.Join(root, "stock", "tests", "flows.yaml"), stock)
	writeFile(t, filepath.Join(root, "testdata", "ghost", "tests", "flows.yaml"),
		"specrig: flows/1\nservice: ghost\ninputs:\n  - message: http GET /ghost\n    branches:\n      - sends: [grpc /stock.S/Reserve]\n        cases: [g]\n")
	foreign := filepath.Join(root, "editor", "tests", "flows.yaml")
	writeFile(t, foreign, "nodes: []\n")

	files, skipped, err := Assemble(root, filepath.Join(root, "docs", "flows.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	pages := slices.Sorted(maps.Keys(files))
	if !slices.Contains(pages, filepath.Join(root, "docs", "shop-http-post-orders.md")) || slices.Contains(pages, filepath.Join(root, "docs", "ghost-http-get-ghost.md")) {
		t.Errorf("want shop's flow and not testdata's: %v", pages)
	}
	if want := []string{foreign}; !reflect.DeepEqual(skipped, want) {
		t.Errorf("skipped %v, want %v", skipped, want)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
