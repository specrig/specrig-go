package flows

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestARunRemovesAPageNoFlowWrites(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "shop", "tests", "flows.yaml"), shop)
	writeFile(t, filepath.Join(root, "stock", "tests", "flows.yaml"), stock)
	index := filepath.Join(root, "docs", "flows.yaml")

	if err := Run(root, index, io.Discard); err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(filepath.Join(root, "docs", "shop-http-post-orders.md"))
	if err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(root, "docs", "shop-http-delete-orders.md")
	writeFile(t, stale, string(page))
	if err := Run(root, index, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a page no flow writes is still there: %v", err)
	}
}

func TestAnIndexThatDoesNotSayItIsOneIsNotOverwritten(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "shop", "tests", "flows.yaml"), shop)
	index := filepath.Join(root, "docs", "flows.yaml")
	writeFile(t, index, "flows:\n  - id: shop-http-post-orders\n    title: Placing an order\n")

	if err := Run(root, index, io.Discard); err == nil {
		t.Fatal("an index without specrig: was overwritten")
	}
}
