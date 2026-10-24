package replacements

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	waffle "gitlab.com/gorib/waffle/container"
)

// Seams are handed in rather than read off a container: what is under test here
// is the comparison, not the parsing.
func check(seams []waffle.Seam, found Inspection, cfg Config) error {
	root, err := ModuleRoot()
	if err != nil {
		return err
	}
	found.Seams = seams
	_, err = Verify(root, found, cfg)
	return err
}

const module = "example.test"

var seams = []waffle.Seam{
	{Field: "SqlDb", Types: []waffle.TypeRef{{Path: module + "/pkg/sql", Name: "Db"}}},
	{Field: "SecurityAdapter", Types: []waffle.TypeRef{{Path: module + "/domain/service", Name: "SecurityAdapter"}}},
	{Field: "Merged", Types: []waffle.TypeRef{
		{Path: module + "/domain/a", Name: "Management"},
		{Path: module + "/domain/b", Name: "Management"},
	}},
}

func allow(entries map[string][]string) Config {
	return Config{Allow: entries}
}

const literal = `package feature

import "example.test/config/diogen"

func boot() {
	_, _ = diogen.InitAppWith(diogen.Replacements{
		SecurityAdapter: nil,
	})
}
`

func TestRefusesAFieldTheListDoesNotGrant(t *testing.T) {
	write(t, map[string]string{"tests/feature/steps_test.go": literal})

	err := check(seams, containers(), allow(nil))

	if err == nil {
		t.Fatal("check passed a field the list does not grant")
	}
	for _, want := range []string{
		"SecurityAdapter",
		module + "/domain/service.SecurityAdapter",
		"tests/feature/steps_test.go:7",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q:\n%s", want, err)
		}
	}
}

// Named by import path and interface, not by field: the field name is diogen's
// and changes when registrations do.
func TestPassesAGrantedField(t *testing.T) {
	write(t, map[string]string{"tests/feature/steps_test.go": literal})

	cfg := allow(map[string][]string{module + "/domain/service": {"SecurityAdapter"}})
	if err := check(seams, containers(), cfg); err != nil {
		t.Fatalf("check refused a granted field: %v", err)
	}
}

// Each of these was a hole while the scan read syntax only.
func TestReadsEveryFormOfWrite(t *testing.T) {
	for name, body := range map[string]string{
		"field by field": `var cut diogen.Replacements
	cut.SqlDb = nil`,

		"inside a branch": `var cut diogen.Replacements
	if true {
		cut.SqlDb = nil
	}`,

		"inside a closure": `var cut diogen.Replacements
	func() { cut.SqlDb = nil }()`,

		"through a pointer": `cut := &diogen.Replacements{}
	cut.SqlDb = nil`,

		"pointer to a local": `var base diogen.Replacements
	cut := &base
	cut.SqlDb = nil`,

		"returned by a helper": `cut := makeCut()
	cut.SqlDb = nil`,

		"field of a scenario struct": `w := world{}
	w.cut.SqlDb = nil`,

		"element of a slice": `cuts := []diogen.Replacements{{}}
	cuts[0].SqlDb = nil`,
	} {
		t.Run(name, func(t *testing.T) {
			write(t, map[string]string{"tests/feature/steps_test.go": `package feature

import "example.test/config/diogen"

type world struct{ cut diogen.Replacements }

func makeCut() diogen.Replacements { return diogen.Replacements{} }

func boot() {
	` + body + `
}
`})

			err := check(seams, containers(), allow(nil))
			if err == nil {
				t.Fatal("the write was not read as a replacement")
			}
			if !strings.Contains(err.Error(), "SqlDb") {
				t.Errorf("the refusal does not name SqlDb:\n%s", err)
			}
		})
	}
}

// The layout a scenario suite grows into on its own, and per-file name
// resolution used to miss it.
func TestReadsAHolderFromAnotherFile(t *testing.T) {
	write(t, map[string]string{
		"tests/feature/world_test.go": `package feature

import "example.test/config/diogen"

var cut diogen.Replacements
`,
		"tests/feature/steps_test.go": `package feature

func boot() {
	cut.SqlDb = nil
}
`,
	})

	err := check(seams, containers(), allow(nil))

	if err == nil {
		t.Fatal("a holder declared in another file of the package was not read")
	}
	if !strings.Contains(err.Error(), "SqlDb") {
		t.Errorf("the refusal does not name SqlDb:\n%s", err)
	}
}

func TestIgnoresTheHarness(t *testing.T) {
	write(t, map[string]string{"tests/internal/harness/harness.go": `package harness

import "example.test/config/diogen"

func Boot(cut diogen.Replacements) {
	cut.SqlDb = nil
}
`})

	if err := check(seams, containers(), allow(nil)); err != nil {
		t.Fatalf("check read the harness: %v", err)
	}
}

func TestIgnoresTheGeneratedContainerOnly(t *testing.T) {
	root := write(t, map[string]string{
		"config/diogen/app_test.go": `package diogen_test

import "example.test/config/diogen"

func boot() {
	_, _ = diogen.InitAppWith(diogen.Replacements{SecurityAdapter: nil})
}
`,
	})

	found, err := scan(root, containers())
	if err != nil {
		t.Fatal(err)
	}

	if len(found) != 1 || found[0].Field != "SecurityAdapter" {
		t.Fatalf("scan found %v, want only the field the test replaces", found)
	}
}

func TestRefusesAPartlyGrantedComposite(t *testing.T) {
	write(t, map[string]string{"tests/feature/steps_test.go": `package feature

import "example.test/config/diogen"

func boot() {
	_, _ = diogen.InitAppWith(diogen.Replacements{Merged: nil})
}
`})

	cfg := allow(map[string][]string{module + "/domain/a": {"Management"}})
	err := check(seams, containers(), cfg)

	if err == nil {
		t.Fatal("check passed a composite with an ungranted constituent")
	}
	if !strings.Contains(err.Error(), "domain/b.Management") {
		t.Errorf("the refusal does not name the ungranted constituent:\n%s", err)
	}
	if strings.Contains(err.Error(), "domain/a.Management") {
		t.Errorf("the refusal names the granted constituent:\n%s", err)
	}
}

// Only reachable through --container: a stale field in the same module would
// fail to compile long before this.
func TestFailsOnAFieldTheSeamsDoNotCarry(t *testing.T) {
	write(t, map[string]string{
		"config/diogen/di_gen.go": `package diogen

type Replacements struct{ Gone any }

type Application struct{}

func InitAppWith(Replacements) (*Application, error) { return &Application{}, nil }
`,
		"tests/feature/steps_test.go": `package feature

import "example.test/config/diogen"

func boot() {
	_, _ = diogen.InitAppWith(diogen.Replacements{Gone: nil})
}
`})

	err := check(seams, containers(), allow(nil))

	if err == nil || !strings.Contains(err.Error(), "no field Gone") {
		t.Fatalf("err = %v, want a failure naming the unknown field", err)
	}
}

// Reading it would mean trusting the field order of a generated struct.
func TestRefusesALiteralWithoutFieldNames(t *testing.T) {
	write(t, map[string]string{
		"config/diogen/di_gen.go": `package diogen

type Replacements struct{ SqlDb any }

type Application struct{}

func InitAppWith(Replacements) (*Application, error) { return &Application{}, nil }
`,
		"tests/feature/steps_test.go": `package feature

import "example.test/config/diogen"

func boot() {
	_, _ = diogen.InitAppWith(diogen.Replacements{nil})
}
`,
	})

	err := check(seams, containers(), allow(nil))

	if err == nil || !strings.Contains(err.Error(), "without field names") {
		t.Fatalf("err = %v, want a failure naming the unkeyed literal", err)
	}
}

func TestIgnoresAnUnrelatedReplacementsStruct(t *testing.T) {
	write(t, map[string]string{"tests/feature/steps_test.go": `package feature

import "example.test/pkg/other"

func boot() {
	_ = other.Replacements{SqlDb: nil}
}
`})

	if err := check(seams, containers(), allow(nil)); err != nil {
		t.Fatalf("check read a struct from another package: %v", err)
	}
}

// It has to survive the passing check, or nobody would ever see it.
func TestWarnsAboutAGrantNoContainerExposes(t *testing.T) {
	root := write(t, map[string]string{"tests/feature/steps_test.go": literal})

	cfg := allow(map[string][]string{module + "/domain/service": {"SecurityAdapter", "Renamed"}})
	found := containers()
	found.Seams = seams
	rep, err := Verify(root, found, cfg)

	if err != nil {
		t.Fatalf("check refused a granted field: %v", err)
	}
	if len(rep.Warnings) != 1 || !strings.Contains(rep.Warnings[0], module+"/domain/service.Renamed") {
		t.Fatalf("warnings = %v, want the one entry nothing exposes", rep.Warnings)
	}
}

func TestRefusesToAnswerOnAModuleThatDoesNotBuild(t *testing.T) {
	write(t, map[string]string{"tests/feature/steps_test.go": `package feature

func boot() { undefinedFunction() }
`})

	err := check(seams, containers(), allow(nil))

	if err == nil || !strings.Contains(err.Error(), "does not type-check") {
		t.Fatalf("err = %v, want a refusal to answer", err)
	}
}

func containers() Inspection {
	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return Inspection{
		Paths: []string{module + "/config/diogen"},
		Files: []string{filepath.Join(cwd, "config", "diogen", "di_gen.go")},
	}
}

// A module that really compiles, because the scan type-checks it.
var fixture = map[string]string{
	"go.mod": "module " + module + "\n\ngo 1.27\n",

	"pkg/sql/sql.go":            "package sql\n\ntype Db interface{ Ping() }\n",
	"domain/service/service.go": "package service\n\ntype SecurityAdapter interface{ Grant() }\n",
	"domain/a/a.go":             "package a\n\ntype Management interface{ A() }\n",
	"domain/b/b.go":             "package b\n\ntype Management interface{ B() }\n",

	// Another package's Replacements is another struct.
	"pkg/other/other.go": "package other\n\ntype Replacements struct{ SqlDb any }\n",

	// The same three seams the hand-built list carries, so a test that reads the
	// container and one that is handed the seams cannot disagree.
	"config/diogen/di_gen.go": `package diogen

import (
	a "example.test/domain/a"
	b "example.test/domain/b"
	service "example.test/domain/service"
	sql "example.test/pkg/sql"
)

type Replacements struct {
	SqlDb           sql.Db
	SecurityAdapter service.SecurityAdapter
	Merged          interface {
		a.Management
		b.Management
	}
}

type Application struct{}

func InitAppWith(Replacements) (*Application, error) { return &Application{}, nil }
`,
}

// Chdir, because the check walks up to go.mod from the working directory.
// GOWORK off, because the fixture sits outside any workspace this repository has.
func write(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range fixture {
		if _, supplied := files[name]; !supplied {
			files[name] = body
		}
	}
	for name, body := range files {
		file := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GOWORK", "off")
	t.Chdir(root)
	return root
}
