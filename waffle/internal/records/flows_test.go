package records

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func service(t *testing.T, root, name, doc string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Join(dir, "tests"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tests", "flows.yaml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAWriteRemovesAPageNoFlowWrites(t *testing.T) {
	root := t.TempDir()
	service(t, root, "shop", "specrig: flows/1\nservice: shop\ninputs:\n  - message: http POST /orders\n    branches:\n      - sends: [grpc /stock.S/Reserve]\n        cases: [a]\n")
	service(t, root, "stock", "specrig: flows/1\nservice: stock\ninputs:\n  - message: grpc /stock.S/Reserve\n    branches:\n      - cases: [b]\n")
	index := filepath.Join(root, "docs", "flows.yaml")

	if err := NewFlows().Run(t.Context(), root, index); err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(filepath.Join(root, "docs", "shop-http-post-orders.md"))
	if err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(root, "docs", "shop-http-delete-orders.md")
	if err := os.WriteFile(stale, page, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := NewFlows().Run(t.Context(), root, index); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a page no flow writes is still there: %v", err)
	}
}
