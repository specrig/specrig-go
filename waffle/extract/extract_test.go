package extract

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/specrig/specrig-go/surface"
)

// The cases are modules of their own, outside the workspace a run may be in.
func TestMain(m *testing.M) {
	_ = os.Setenv("GOWORK", "off")
	os.Exit(m.Run())
}

func TestExtractReadsWhatCrossesTheEdge(t *testing.T) {
	got, err := Extract(filepath.Join("testdata", "cases", "shop"))
	if err != nil {
		t.Fatal(err)
	}
	want := surface.Surface{
		Specrig:   surface.Format,
		Service:   "shop",
		Publishes: []string{"orders.canceled", "orders.created", "orders.placed", "orders.updated", "refunds.issued"},
		Consumes:  []string{"payments.refunded", "payments.succeeded", "stock.#"},
		Serves: []string{"/order.OrderService/Place", "/refund.RefundService/Issue",
			"DELETE /admin/users/{userId}", "GET /admin/users/{userId}", "GET /health", "POST /customers/password"},
		Calls: []string{"/audit.AuditService/Record", "/stock.StockService/Reserve",
			"GET /api/documents/{}", "GET external", "POST /api/render/text"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

// merchandize's adapter calls an interface invoice's service satisfies, but the
// container never hands it one: a shape that fits is not a link.
func TestExtractLinksDomainsTheContainerWires(t *testing.T) {
	got, err := Extract(filepath.Join("testdata", "cases", "monolith"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"rent → invoice: Invoices.Issue"}
	if !reflect.DeepEqual(got.Domains, want) {
		t.Errorf("got %v, want %v", got.Domains, want)
	}
}

func TestExtractRefusesAKeyItCannotTell(t *testing.T) {
	dir := t.TempDir()
	outbox, err := filepath.Abs(filepath.Join("testdata", "cases", "shop", "fake", "waffle"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "go.mod"), "module example.com/lost\n\ngo 1.27.0\n\n"+
		"require gitlab.com/gorib/waffle v0.0.0\n\nreplace gitlab.com/gorib/waffle => "+outbox+"\n")
	write(t, filepath.Join(dir, "lost.go"), `package lost

import (
	"context"

	"gitlab.com/gorib/waffle/tools/outbox"
)

func key() string { return "anything" }

func send(ctx context.Context, p outbox.Publisher) error { return p.Enqueue(ctx, key(), nil) }
`)
	_, err = Extract(dir)
	if err == nil || !strings.Contains(err.Error(), "lost.go:11: publishes a name that is not a constant") {
		t.Fatalf("got %v, want a refusal naming lost.go:11", err)
	}
}

func TestExtractWritesAPrefixNamedInFlowsPrefixes(t *testing.T) {
	got, err := Extract(shortener(t, "flows:\n  prefixes:\n    - web.router.path\n"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"GET {}/*"}; !reflect.DeepEqual(got.Serves, want) {
		t.Errorf("got %v, want %v", got.Serves, want)
	}
}

func TestExtractRefusesAPrefixFlowsPrefixesMisses(t *testing.T) {
	_, err := Extract(shortener(t, "flows:\n  prefixes:\n    - web.router.gone\n"))
	for _, want := range []string{
		"web/web.go:8 (web.router.path): routes under a mux whose group prefix cannot be told",
		"specrig.yaml: flows.prefixes names web.router.gone, which no group prefix reads",
	} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("got %v, want %q", err, want)
		}
	}
}

func shortener(t *testing.T, config string) string {
	t.Helper()
	dir := t.TempDir()
	http, err := filepath.Abs(filepath.Join("testdata", "cases", "shop", "fake", "http"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "go.mod"), "module example.com/shortener\n\ngo 1.27.0\n\n"+
		"require gitlab.com/gorib/http v0.0.0\n\nreplace gitlab.com/gorib/http => "+http+"\n")
	write(t, filepath.Join(dir, "specrig.yaml"), config)
	write(t, filepath.Join(dir, "web", "web.go"), `package web

import "gitlab.com/gorib/http/server"

type router struct{ path string }

func (r *router) Setup(mux server.Mux) {
	g := mux.Group(r.path)
	g.Get("/*")
}
`)
	return dir
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
