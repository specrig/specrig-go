package surface

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReadRefusesANewerFormatAndAnotherKind(t *testing.T) {
	file := filepath.Join(t.TempDir(), "surface.yaml")
	for body, want := range map[string]string{
		"specrig: surface/2\nservice: shop\n": "newer than this specrig-go reads",
		"specrig: flows/1\nservice: shop\n":   "not a surface record",
		"<<<<<<< HEAD\nspecrig: surface/1\n":  "yaml:",
	} {
		write(t, file, body)
		if _, err := Read(file); err == nil || !strings.Contains(err.Error(), want) || errors.Is(err, ErrForeign) {
			t.Errorf("%q: got %v, want %q", body, err, want)
		}
	}
}

func TestReadTellsAFileThatDoesNotSayItIsARecord(t *testing.T) {
	file := filepath.Join(t.TempDir(), "surface.yaml")
	for _, body := range []string{"version: 1\nservice: shop\n", "- a list\n", ""} {
		write(t, file, body)
		if _, err := Read(file); !errors.Is(err, ErrForeign) {
			t.Errorf("%q: got %v, want ErrForeign", body, err)
		}
	}
}

func TestEncodeReadsBack(t *testing.T) {
	s := Surface{Specrig: Format, Service: "shop", Publishes: []string{"a.b"}, Calls: []string{"/x.Y/Z"}}
	file := filepath.Join(t.TempDir(), "surface.yaml")
	write(t, file, string(s.Encode()))
	got, err := Read(file)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, s) {
		t.Errorf("got %+v, want %+v", got, s)
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
