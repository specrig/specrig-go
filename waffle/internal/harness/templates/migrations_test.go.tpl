package tests

import (
	"testing"

	"{{.Module}}/tests/internal/harness"
)

func TestMigrationsAreIndependent(t *testing.T) {
	if err := harness.CheckMigrations(t.Context()); err != nil {
		t.Fatal(err)
	}
}
