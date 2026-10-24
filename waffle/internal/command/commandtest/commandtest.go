package commandtest

import (
	"os"
	"testing"

	"github.com/specrig/specrig-go/waffle/internal/command"
)

// Project is an empty module in a fresh directory, which is also the working one.
func Project(t *testing.T, module string) *command.Project {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := os.WriteFile("go.mod", []byte("module "+module+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := command.OpenProject()
	if err != nil {
		t.Fatal(err)
	}
	return p
}
