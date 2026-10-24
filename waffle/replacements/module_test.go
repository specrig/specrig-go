package replacements

import (
	"strings"
	"testing"
)

// Nothing handed in but the tree — every other test in this package supplies
// the containers, the seams or the grants itself.
func TestCheckReadsTheModuleAndTheList(t *testing.T) {
	tree := func(allow string) map[string]string {
		return map[string]string{
			"config/di/di.go": "//go:generate diogen -output config/diogen/di_gen.go $GOFILE\n\npackage di\n",
			"specrig.yaml":    "replacements:\n  allow:\n    example.test/pkg/sql: [" + allow + "]\n",
			"tests/feature/steps_test.go": `package feature

import "example.test/config/diogen"

func boot() {
	_ = diogen.Replacements{SqlDb: nil}
}
`,
		}
	}

	t.Run("granted", func(t *testing.T) {
		write(t, tree("Db"))
		if err := Check(); err != nil {
			t.Fatalf("check refused a granted field: %v", err)
		}
	})

	// The same tree, granting a different interface in the same package: what the
	// list names is what counts, not that it mentions the package at all.
	t.Run("not granted", func(t *testing.T) {
		write(t, tree("Tx"))
		err := Check()
		if err == nil {
			t.Fatal("check passed a field that replacements.allow does not name")
		}
		for _, want := range []string{"SqlDb", "example.test/pkg/sql.Db", "tests/feature/steps_test.go:6"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal does not name %q:\n%s", want, err)
			}
		}
	})

	// No specrig.yaml at all is the state a service starts in, and it is the case
	// the rule was written for: absent grants nothing.
	t.Run("no file", func(t *testing.T) {
		files := tree("Db")
		delete(files, "specrig.yaml")
		write(t, files)

		err := Check()
		if err == nil {
			t.Fatal("a module with no specrig.yaml replaced a seam nobody granted")
		}
		if !strings.Contains(err.Error(), "example.test/pkg/sql.Db") {
			t.Errorf("the module failed for some other reason than the missing grant: %v", err)
		}
	})
}
