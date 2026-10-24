package {{.Package}}_test

import (
	"testing"

	"github.com/specrig/specrig-go/waffle/replacements"
)

// A seam the list does not grant is a question for whoever owns specrig.yaml —
// granting it yourself is answering it yourself.
// One copy answers for the whole module: the check walks up to go.mod.
func TestReplacementsStayInsideTheList(t *testing.T) {
	if err := replacements.Check(); err != nil {
		t.Fatal(err)
	}
}
