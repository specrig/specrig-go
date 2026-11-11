package standin

import (
	"context"
	"strings"
	"testing"

	"github.com/specrig/specrig-go/trace"
)

func records(t *testing.T, since int) []trace.Record {
	t.Helper()
	return trace.Records()[since:]
}

func TestTraceNamesARequestByTheContractsRoute(t *testing.T) {
	trace.Enable("shop")
	since := len(trace.Records())
	b, c := newBackend(t, Options{Name: "orders", Contract: orders})
	b.Reply("GET", "/orders/{id}", 200, map[string]any{"id": "42", "sku": "a"})
	b.Reply("POST", "/orders", 201, map[string]any{"id": "43", "sku": "b"})
	b.Reply("GET", "/orders/latest", 200, map[string]any{"id": "44", "sku": "c"})
	b.Reply("GET", "/nope", 200, nil)

	send(t, c, "GET", "/orders/42?x=1", "")
	send(t, c, "POST", "/orders", `{"sku":"b","qty":1}`)
	send(t, c, "GET", "/orders/latest", "")
	send(t, c, "GET", "/nope?x=1", "")

	got := records(t, since)
	if len(got) != 4 {
		t.Fatalf("%d records: %+v", len(got), got)
	}
	for i, want := range []struct{ name, request, response string }{
		{"GET /orders/{id}", "", `{"id":"42","sku":"a"}`},
		{"POST /orders", `{"sku":"b","qty":1}`, `{"id":"43","sku":"b"}`},
		{"GET /orders/latest", "", `{"id":"44","sku":"c"}`},
		{"GET /nope", "", `{}`},
	} {
		r := got[i]
		if r.Dir != "out" || r.Kind != "http" || r.Service != "shop" {
			t.Errorf("record %d = %+v", i, r)
		}
		if r.Name != want.name || string(r.Request) != want.request || string(r.Response) != want.response {
			t.Errorf("record %d = %s %q %q, want %s %q %q", i, r.Name, r.Request, r.Response, want.name, want.request, want.response)
		}
	}
}

func TestTraceNamesARequestWithoutAContractByItsPath(t *testing.T) {
	trace.Enable("shop")
	since := len(trace.Records())
	b, c := newBackend(t, Options{})
	b.Reply("GET", "/orders/{id}", 200, map[string]any{"id": "42"})

	send(t, c, "GET", "/orders/42?x=1", "")

	got := records(t, since)
	if len(got) != 1 || got[0].Name != "GET /orders/42" || string(got[0].Response) != `{"id":"42"}` {
		t.Errorf("records = %+v", got)
	}
}

func TestTraceNamesWhatABaseServerPrefixesByTheRouteAsServed(t *testing.T) {
	trace.Enable("shop")
	since := len(trace.Records())
	b, c := newBackend(t, Options{Name: "things", Contract: based})
	b.Reply("GET", "/api/v1/things/{id}", 200, map[string]any{"id": "7"})

	send(t, c, "GET", "/api/v1/things/7", "")

	if got := records(t, since); len(got) != 1 || got[0].Name != "GET /api/v1/things/{id}" {
		t.Errorf("records = %+v", got)
	}
}

func TestTraceKeepsATimedOutRequestWithoutAnAnswer(t *testing.T) {
	trace.Enable("shop")
	since := len(trace.Records())
	b, c := newBackend(t, Options{})
	b.TimeOut("POST", "/orders")

	if _, err := c.Post(host+"/orders", "application/json", strings.NewReader(`{"sku":"a"}`)); err == nil {
		t.Fatal("the call did not time out")
	}

	got := records(t, since)
	if len(got) != 1 || got[0].Name != "POST /orders" || string(got[0].Request) != `{"sku":"a"}` || got[0].Response != nil {
		t.Errorf("records = %+v", got)
	}
}

func TestTraceOrdersACallByWhenItArrived(t *testing.T) {
	trace.Enable("shop")
	since := len(trace.Records())
	b, c := newBackend(t, Options{})
	b.Reply("GET", "/slow", 200, nil)
	b.Reply("GET", "/quick", 200, nil)
	b.Hold("GET", "/slow")

	held := async(context.Background(), c, "GET", "/slow")
	eventually(t, "the request to arrive", func() bool { return len(b.Requests("GET", "/slow")) == 1 })
	send(t, c, "GET", "/quick", "")
	if err := b.Release("GET", "/slow"); err != nil {
		t.Fatal(err)
	}
	receive(t, held)

	got := records(t, since)
	if len(got) != 2 || got[0].Name != "GET /quick" || got[1].Name != "GET /slow" {
		t.Fatalf("records = %+v", got)
	}
	if got[1].Seq >= got[0].Seq {
		t.Errorf("the held call began at %d, the quick one at %d", got[1].Seq, got[0].Seq)
	}
}
