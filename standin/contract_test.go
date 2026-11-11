package standin

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const (
	orders = "testdata/orders.yaml"
	based  = "testdata/based.yaml"
)

func violations(t *testing.T, b *Backend) string {
	t.Helper()
	got := b.Failures()
	if len(got) == 0 {
		return ""
	}
	if len(got) != 1 || !strings.HasPrefix(got[0], "what orders's contract, ") {
		t.Fatalf("failures = %q, want only the contract's", got)
	}
	return got[0]
}

func TestWhatTheContractAllowsIsNotAFailure(t *testing.T) {
	b, c := newBackend(t, Options{Name: "orders", Contract: orders})
	b.Reply("GET", "/orders", 200, []map[string]any{{"id": "1", "sku": "a", "qty": 2}})
	b.ReplyTo("GET", "/orders", url.Values{"limit": {"5"}}, 200, []any{})
	b.Reply("POST", "/orders", 201, map[string]any{"id": "1", "sku": "a"})
	b.Reply("GET", "/orders/latest", 200, map[string]any{"id": "2", "sku": "b"})
	b.Reply("GET", "/orders/{id}", 404, map[string]any{"code": "not_found"})
	b.Reply("DELETE", "/orders/{id}", 204, nil)
	b.Reply("GET", "/ping", 204, nil)

	send(t, c, "GET", "/orders", "")
	send(t, c, "GET", "/orders?limit=5", "")
	send(t, c, "POST", "/orders", `{"sku":"a","qty":2}`)
	send(t, c, "GET", "/orders/latest", "")
	send(t, c, "GET", "/orders/7", "")
	send(t, c, "DELETE", "/orders/7", "")
	send(t, c, "GET", "/ping", "")

	if got := b.Failures(); len(got) != 0 {
		t.Errorf("failures = %q", got)
	}
}

func TestARouteTheContractLacksIsAViolation(t *testing.T) {
	b, c := newBackend(t, Options{Name: "orders", Contract: orders})
	b.Reply("GET", "/nope", 200, nil)
	b.Reply("PUT", "/orders", 200, nil)

	send(t, c, "GET", "/nope", "")
	send(t, c, "PUT", "/orders", "")

	got := violations(t, b)
	for _, want := range []string{
		"GET /nope: orders serves no such route",
		"PUT /orders: orders serves no such route",
		"orders's contract, " + orders + ", does not allow: ",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("failure %q lacks %q", got, want)
		}
	}
}

func TestARequestBodyAgainstTheSchema(t *testing.T) {
	b, c := newBackend(t, Options{Name: "orders", Contract: orders})
	b.Reply("POST", "/orders", 201, map[string]any{"id": "1", "sku": "a"})

	send(t, c, "POST", "/orders", `{"sku":"a","qty":2}`)
	if got := violations(t, b); got != "" {
		t.Fatalf("a valid body: %s", got)
	}

	send(t, c, "POST", "/orders", `{"sku":"a","qty":0}`)
	got := violations(t, b)
	if !strings.Contains(got, "POST /orders: request body has an error") || !strings.Contains(got, "/qty") {
		t.Errorf("failure = %s", got)
	}
	if strings.Contains(got, "\n") || strings.Contains(got, "Schema:") {
		t.Errorf("failure = %q, want one line without the schema dumped into it", got)
	}
}

func TestAParameterAgainstTheSchema(t *testing.T) {
	b, c := newBackend(t, Options{Name: "orders", Contract: orders})
	b.Reply("GET", "/orders", 200, []any{})

	send(t, c, "GET", "/orders?limit=0", "")

	if got := violations(t, b); !strings.Contains(got, `GET /orders: parameter "limit" in query has an error`) {
		t.Errorf("failure = %s", got)
	}
}

func TestAnAnswerAgainstTheSchema(t *testing.T) {
	b, c := newBackend(t, Options{Name: "orders", Contract: orders})
	b.Reply("GET", "/orders/1", 200, map[string]any{"sku": 5})
	b.Reply("GET", "/orders/2", 200, []byte(`{"id":"2","sku":"a"}`))

	send(t, c, "GET", "/orders/1", "")
	send(t, c, "GET", "/orders/2", "")

	got := violations(t, b)
	if !strings.Contains(got, "GET /orders/1 answered 200: response body doesn't match schema") {
		t.Errorf("failure = %s", got)
	}
	if !strings.Contains(got, "GET /orders/2 answered 200: response header Content-Type has unexpected value") {
		t.Errorf("failure = %s", got)
	}
}

func TestAnUndocumentedStatusIsAViolation(t *testing.T) {
	b, c := newBackend(t, Options{Name: "orders", Contract: orders})
	b.Reply("GET", "/orders/1", 418, map[string]any{"code": "teapot"})

	send(t, c, "GET", "/orders/1", "")

	if got := violations(t, b); !strings.Contains(got, "GET /orders/1 answered 418: status is not supported") {
		t.Errorf("failure = %s", got)
	}
}

func TestAFailureMayAnswerAnyRouteWithItsSchema(t *testing.T) {
	b, c := newBackend(t, Options{Name: "orders", Contract: orders, Failure: "Failure"})
	b.Reply("GET", "/orders/1", 500, map[string]any{"code": "boom", "message": "it broke"})
	b.Reply("POST", "/orders", 502, map[string]any{"code": "boom", "message": "it broke"})
	send(t, c, "GET", "/orders/1", "")
	send(t, c, "POST", "/orders", `{"sku":"a","qty":1}`)
	if got := violations(t, b); got != "" {
		t.Fatalf("what the schema allows: %s", got)
	}

	b.Reply("GET", "/orders/2", 500, map[string]any{"oops": 1})
	b.Reply("GET", "/orders/3", 500, []byte("not json"))
	send(t, c, "GET", "/orders/2", "")
	send(t, c, "GET", "/orders/3", "")
	got := violations(t, b)
	if !strings.Contains(got, "GET /orders/2 answered 500: the failure does not match its schema") {
		t.Errorf("failure = %s", got)
	}
	if !strings.Contains(got, "GET /orders/3 answered 500: the failure is not JSON") {
		t.Errorf("failure = %s", got)
	}
}

func TestADocumentedFiveHundredIsCheckedByTheDocument(t *testing.T) {
	b, c := newBackend(t, Options{Name: "orders", Contract: orders, Failure: "Failure"})
	// Problem needs a code only; Failure would want a message as well.
	b.Reply("GET", "/ping", 503, map[string]any{"code": "starting"})
	send(t, c, "GET", "/ping", "")
	if got := violations(t, b); got != "" {
		t.Fatalf("failure = %s", got)
	}

	b.Reply("GET", "/ping", 503, map[string]any{"message": "starting"})
	send(t, c, "GET", "/ping", "")
	if got := violations(t, b); !strings.Contains(got, "GET /ping answered 503: response body doesn't match schema") {
		t.Errorf("failure = %s", got)
	}
}

func TestAFailureSchemaDoesNotExcuseWhatIsNotAFiveHundred(t *testing.T) {
	b, c := newBackend(t, Options{Name: "orders", Contract: orders, Failure: "Failure"})
	b.Reply("GET", "/orders/1", 418, map[string]any{"code": "teapot", "message": "short and stout"})

	send(t, c, "GET", "/orders/1", "")

	if got := violations(t, b); !strings.Contains(got, "GET /orders/1 answered 418: status is not supported") {
		t.Errorf("failure = %s", got)
	}
}

func TestWithoutAFailureSchemaAFiveHundredIsUndocumented(t *testing.T) {
	b, c := newBackend(t, Options{Name: "orders", Contract: orders})
	b.Reply("GET", "/orders/1", 500, map[string]any{"code": "boom", "message": "it broke"})

	send(t, c, "GET", "/orders/1", "")

	if got := violations(t, b); !strings.Contains(got, "GET /orders/1 answered 500: status is not supported") {
		t.Errorf("failure = %s", got)
	}
}

func TestWhatNoStepAnsweredIsNotCheckedAgainstTheContract(t *testing.T) {
	b, c := newBackend(t, Options{Name: "orders", Contract: orders})

	send(t, c, "GET", "/orders/1", "")

	want := "orders was called where no step declared its answer: GET /orders/1"
	if got := b.Failures(); len(got) != 1 || got[0] != want {
		t.Errorf("failures = %q, want only %q", got, want)
	}
}

func TestAServersBasePathIsStrippedFromTheRequest(t *testing.T) {
	b, c := newBackend(t, Options{Name: "orders", Contract: based})
	b.Reply("GET", "/api/v1/things/{id}", 200, map[string]any{"id": "7"})
	b.Reply("GET", "/things/{id}", 200, map[string]any{"id": "7"})

	send(t, c, "GET", "/api/v1/things/7", "")
	if got := violations(t, b); got != "" {
		t.Fatalf("with the base path: %s", got)
	}

	send(t, c, "GET", "/things/7", "")
	if got := violations(t, b); !strings.Contains(got, "GET /things/7: orders serves no such route") {
		t.Errorf("without the base path: %s", got)
	}
}

func TestFailuresComeInOrder(t *testing.T) {
	b, c := newBackend(t, Options{Name: "orders", Contract: orders})
	b.Reply("GET", "/nope", 200, nil)
	b.Reply("GET", "/orders/2", 200, map[string]any{"id": "2", "sku": "a"})
	b.Hold("GET", "/orders/2")

	send(t, c, "GET", "/nope", "")
	send(t, c, "GET", "/orders/1", "")
	held := async(context.Background(), c, "GET", "/orders/2")
	eventually(t, "the request to arrive", func() bool { return len(b.Requests("GET", "/orders/2")) == 1 })

	got := b.Failures()
	if len(got) != 3 {
		t.Fatalf("failures = %q", got)
	}
	for i, prefix := range []string{
		"orders was called where no step declared its answer: GET /orders/1",
		"orders holds what no step released: GET /orders/2",
		"what orders's contract, " + orders + ", does not allow: GET /nope: orders serves no such route",
	} {
		if !strings.HasPrefix(got[i], prefix) {
			t.Errorf("failure %d = %q, want it to start with %q", i, got[i], prefix)
		}
	}

	if err := b.Release("GET", "/orders/2"); err != nil {
		t.Fatal(err)
	}
	receive(t, held)
}

func TestAContractIsReadOnceForEveryBackend(t *testing.T) {
	first, err := New(Options{Contract: orders})
	if err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs(orders)
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(Options{Contract: abs, Failure: "Failure"})
	if err != nil {
		t.Fatal(err)
	}
	if first.contract != second.contract {
		t.Error("the document was read twice")
	}
}

func TestADocumentThatCannotBeReadIsAnError(t *testing.T) {
	broken := filepath.Join(t.TempDir(), "broken.yaml")
	if err := os.WriteFile(broken, []byte("openapi: 3.0.3\ninfo: {title: x, version: '1'}\npaths: 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	missingResponses := filepath.Join(t.TempDir(), "invalid.yaml")
	if err := os.WriteFile(missingResponses, []byte("openapi: 3.0.3\ninfo: {title: x, version: '1'}\npaths:\n  /x:\n    get: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for name, o := range map[string]Options{
		"missing":           {Name: "orders", Contract: filepath.Join(t.TempDir(), "absent.yaml")},
		"broken":            {Name: "orders", Contract: broken},
		"invalid":           {Name: "orders", Contract: missingResponses},
		"no failure schema": {Name: "orders", Contract: orders, Failure: "Absent"},
	} {
		if _, err := New(o); err == nil || !strings.Contains(err.Error(), "orders's contract") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := New(Options{Failure: "Absent"}); err != nil {
		t.Errorf("a failure without a contract: err = %v", err)
	}
}

func TestConcurrentCallsAreChecked(t *testing.T) {
	b, c := newBackend(t, Options{Name: "orders", Contract: orders})
	b.Reply("GET", "/orders/{id}", 200, map[string]any{"id": "1", "sku": "a"})
	b.Reply("POST", "/orders", 201, map[string]any{"id": "1", "sku": "a"})

	const callers, calls = 16, 25
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range calls {
				get, _ := http.NewRequest("GET", host+"/orders/1", nil)
				post, _ := http.NewRequest("POST", host+"/orders", strings.NewReader(`{"sku":"a","qty":1}`))
				post.Header.Set("Content-Type", "application/json")
				for _, req := range []*http.Request{get, post} {
					resp, err := c.Do(req)
					if err != nil {
						t.Error(err)
						return
					}
					resp.Body.Close()
				}
			}
		}()
	}
	wg.Wait()

	if got := len(b.Requests("GET", "/orders/{id}")); got != callers*calls {
		t.Errorf("%d GETs kept, want %d", got, callers*calls)
	}
	if got := len(b.Requests("POST", "/orders")); got != callers*calls {
		t.Errorf("%d POSTs kept, want %d", got, callers*calls)
	}
	if got := b.Failures(); len(got) != 0 {
		t.Errorf("failures = %q", got)
	}
}
