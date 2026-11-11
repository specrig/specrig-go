package standin

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

const host = "http://neighbour.test"

func newBackend(t *testing.T, o Options) (*Backend, *http.Client) {
	t.Helper()
	b, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return b, &http.Client{Transport: b, Timeout: 5 * time.Second}
}

func send(t *testing.T, c *http.Client, method, target, body string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, host+target, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(raw)
}

type outcome struct {
	status int
	body   string
	err    error
}

func async(ctx context.Context, c *http.Client, method, target string) <-chan outcome {
	done := make(chan outcome, 1)
	go func() {
		req, err := http.NewRequestWithContext(ctx, method, host+target, nil)
		if err != nil {
			done <- outcome{err: err}
			return
		}
		resp, err := c.Do(req)
		if err != nil {
			done <- outcome{err: err}
			return
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		done <- outcome{status: resp.StatusCode, body: string(raw), err: err}
	}()
	return done
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func receive(t *testing.T, done <-chan outcome) outcome {
	t.Helper()
	select {
	case o := <-done:
		return o
	case <-time.After(5 * time.Second):
		t.Fatal("the request was not answered")
		return outcome{}
	}
}

func TestReplyAnswersWithStatusAndJSON(t *testing.T) {
	b, c := newBackend(t, Options{})
	b.Reply("GET", "/orders/1", 202, map[string]any{"id": "1"})

	resp, body := send(t, c, "GET", "/orders/1", "")

	if resp.StatusCode != 202 || resp.Status != "202 Accepted" {
		t.Errorf("status = %d %q", resp.StatusCode, resp.Status)
	}
	if resp.Proto != "HTTP/1.1" || resp.ProtoMajor != 1 || resp.ProtoMinor != 1 {
		t.Errorf("proto = %q", resp.Proto)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("content type = %q", got)
	}
	if body != `{"id":"1"}` {
		t.Errorf("body = %s", body)
	}
	if resp.ContentLength != int64(len(body)) {
		t.Errorf("content length = %d", resp.ContentLength)
	}
	if resp.Request == nil || resp.Request.URL.Path != "/orders/1" {
		t.Errorf("request = %v", resp.Request)
	}
}

func TestReplyWithoutABodyAnswersAnEmptyObject(t *testing.T) {
	b, c := newBackend(t, Options{})
	b.Reply("POST", "/orders", 201, nil)

	resp, body := send(t, c, "POST", "/orders", "")

	if resp.StatusCode != 201 || body != "{}" || resp.Header.Get("Content-Type") != "application/json" {
		t.Errorf("answered %d %s %q", resp.StatusCode, body, resp.Header.Get("Content-Type"))
	}
}

func TestReplyToTakesTheQueryNamingTheMostKeys(t *testing.T) {
	b, c := newBackend(t, Options{})
	b.Reply("GET", "/orders", 200, "any")
	b.ReplyTo("GET", "/orders", url.Values{"status": {"open"}}, 200, "open")
	b.ReplyTo("GET", "/orders", url.Values{"status": {"open"}, "limit": {"1"}}, 200, "open and limit")

	for target, want := range map[string]string{
		"/orders":                     "any",
		"/orders?status=closed":       "any",
		"/orders?limit=1":             "any",
		"/orders?status=open":         "open",
		"/orders?status=open&other=1": "open",
		"/orders?status=open&limit=1": "open and limit",
		"/orders?limit=1&status=open": "open and limit",
		"/orders?limit=2&status=open": "open",
	} {
		if _, body := send(t, c, "GET", target, ""); body != strconv.Quote(want) {
			t.Errorf("%s answered %s, want %q", target, body, want)
		}
	}
}

func TestOfTwoDeclarationsNamingAsManyTheLaterWins(t *testing.T) {
	b, c := newBackend(t, Options{})
	b.ReplyTo("GET", "/orders", url.Values{"tag": {"a"}}, 200, "first value")
	b.ReplyTo("GET", "/orders", url.Values{"tag": {"a", "b"}}, 200, "list")
	b.ReplyTo("GET", "/orders", url.Values{"limit": {"1"}}, 200, "limit")
	for target, want := range map[string]string{
		"/orders?tag=a&tag=b":         "list",
		"/orders?tag=a":               "first value",
		"/orders?tag=a&tag=c":         "first value",
		"/orders?tag=b&tag=a":         "",
		"/orders?tag=a&limit=1":       "limit",
		"/orders?limit=1&tag=a&tag=b": "limit",
	} {
		resp, body := send(t, c, "GET", target, "")
		if want == "" && resp.StatusCode != 501 || want != "" && body != strconv.Quote(want) {
			t.Errorf("%s answered %d %s, want %q", target, resp.StatusCode, body, want)
		}
	}

	reversed, c := newBackend(t, Options{})
	reversed.ReplyTo("GET", "/orders", url.Values{"tag": {"a", "b"}}, 200, "list")
	reversed.ReplyTo("GET", "/orders", url.Values{"tag": {"a"}}, 200, "first value")
	if _, body := send(t, c, "GET", "/orders?tag=a&tag=b", ""); body != `"first value"` {
		t.Errorf("answered %s, the later declaration should win", body)
	}
}

func TestDeclaringAgainForTheSameQueryReplaces(t *testing.T) {
	b, c := newBackend(t, Options{})
	b.Reply("GET", "/orders", 200, "one")
	b.ReplyTo("GET", "/orders", url.Values{"status": {"open"}}, 200, "open one")
	b.Reply("GET", "/orders", 200, "two")
	b.ReplyTo("GET", "/orders", url.Values{"status": {"open"}}, 404, "open two")

	if _, body := send(t, c, "GET", "/orders", ""); body != `"two"` {
		t.Errorf("answered %s", body)
	}
	resp, body := send(t, c, "GET", "/orders?status=open", "")
	if resp.StatusCode != 404 || body != `"open two"` {
		t.Errorf("answered %d %s", resp.StatusCode, body)
	}
}

func TestATemplateTakesOneSegment(t *testing.T) {
	b, c := newBackend(t, Options{})
	b.Reply("GET", "/orders/{id}", 200, "order")

	if _, body := send(t, c, "GET", "/orders/7", ""); body != `"order"` {
		t.Errorf("answered %s", body)
	}
	for _, target := range []string{"/orders", "/orders/", "/orders/7/items", "/shops/7"} {
		if resp, _ := send(t, c, "GET", target, ""); resp.StatusCode != 501 {
			t.Errorf("%s answered %d, want 501", target, resp.StatusCode)
		}
	}
	if resp, _ := send(t, c, "POST", "/orders/7", ""); resp.StatusCode != 501 {
		t.Errorf("POST answered %d, want 501", resp.StatusCode)
	}
}

func TestTheExactPathBeatsATemplate(t *testing.T) {
	for _, exactFirst := range []bool{true, false} {
		b, c := newBackend(t, Options{})
		declare := []func(){
			func() { b.Reply("GET", "/orders/latest", 200, "exact") },
			func() { b.Reply("GET", "/orders/{id}", 200, "template") },
		}
		if !exactFirst {
			slices.Reverse(declare)
		}
		for _, d := range declare {
			d()
		}

		if _, body := send(t, c, "GET", "/orders/latest", ""); body != `"exact"` {
			t.Errorf("exact first %v: answered %s", exactFirst, body)
		}
		if _, body := send(t, c, "GET", "/orders/7", ""); body != `"template"` {
			t.Errorf("exact first %v: answered %s", exactFirst, body)
		}
	}
}

func TestTheFirstDeclaredTemplateWins(t *testing.T) {
	b, c := newBackend(t, Options{})
	b.Reply("GET", "/{kind}/1", 200, "kind")
	b.Reply("GET", "/orders/{id}", 200, "id")

	if _, body := send(t, c, "GET", "/orders/1", ""); body != `"kind"` {
		t.Errorf("answered %s", body)
	}
	if _, body := send(t, c, "GET", "/orders/2", ""); body != `"id"` {
		t.Errorf("answered %s", body)
	}
}

func TestRequestsAreKeptAndFoundByTemplate(t *testing.T) {
	b, c := newBackend(t, Options{})
	b.Reply("GET", "/orders/{id}", 200, nil)
	b.Reply("POST", "/orders", 201, nil)

	req, _ := http.NewRequest("GET", host+"/orders/1?status=open&status=late", nil)
	req.Header.Set("X-Trace", "abc")
	if _, err := c.Do(req); err != nil {
		t.Fatal(err)
	}
	send(t, c, "GET", "/orders/2", "")
	send(t, c, "GET", "/orders/2/items", "")
	send(t, c, "POST", "/orders", `{"sku":"a","qty":2}`)

	if got := len(b.Requests("GET", "/orders/{id}")); got != 2 {
		t.Errorf("by template: %d requests, want 2", got)
	}
	if got := len(b.Requests("GET", "/orders/2")); got != 1 {
		t.Errorf("by path: %d requests, want 1", got)
	}
	if got := len(b.Requests("PUT", "/orders/{id}")); got != 0 {
		t.Errorf("by method: %d requests, want 0", got)
	}

	one := b.Requests("GET", "/orders/1")[0]
	if one.Method != "GET" || one.Path != "/orders/1" || one.Body != nil {
		t.Errorf("received %+v", one)
	}
	if !slices.Equal(one.Query["status"], []string{"open", "late"}) || one.Header.Get("X-Trace") != "abc" {
		t.Errorf("received query %v header %v", one.Query, one.Header)
	}

	var order struct {
		Sku string `json:"sku"`
		Qty int    `json:"qty"`
	}
	if err := b.Requests("POST", "/orders")[0].JSON(&order); err != nil || order.Sku != "a" || order.Qty != 2 {
		t.Errorf("json = %+v, %v", order, err)
	}
	if err := one.JSON(&order); err == nil || !strings.Contains(err.Error(), "GET /orders/1") {
		t.Errorf("json of a request without a body: %v", err)
	}
}

func TestHeadersAreRepeatable(t *testing.T) {
	b, c := newBackend(t, Options{})
	b.Reply("GET", "/orders/1", 200, nil, Header("X-Rate", "9"), Header("set-cookie", "a=1"), Header("Set-Cookie", "b=2"))

	resp, _ := send(t, c, "GET", "/orders/1", "")

	if got := resp.Header.Get("X-Rate"); got != "9" {
		t.Errorf("X-Rate = %q", got)
	}
	if got := resp.Header.Values("Set-Cookie"); !slices.Equal(got, []string{"a=1", "b=2"}) {
		t.Errorf("Set-Cookie = %v", got)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("content type = %q", got)
	}
}

func TestAHeaderNamedContentTypeReplacesTheDefault(t *testing.T) {
	b, c := newBackend(t, Options{})
	b.Reply("GET", "/orders/1", 400, map[string]any{"code": "x"}, Header("content-type", "application/problem+json"))

	resp, body := send(t, c, "GET", "/orders/1", "")

	if got := resp.Header.Values("Content-Type"); !slices.Equal(got, []string{"application/problem+json"}) {
		t.Errorf("content type = %v", got)
	}
	if body != `{"code":"x"}` {
		t.Errorf("body = %s", body)
	}
}

func TestBytesAreSentAsTheyAre(t *testing.T) {
	b, c := newBackend(t, Options{})
	b.Reply("GET", "/files/1", 200, []byte("%PDF-1.7\x00"))
	b.Reply("GET", "/files/2", 200, []byte("<p>hi</p>"), Header("Content-Type", "text/html"))

	resp, body := send(t, c, "GET", "/files/1", "")
	if body != "%PDF-1.7\x00" || resp.Header.Get("Content-Type") != "application/octet-stream" {
		t.Errorf("answered %q %q", body, resp.Header.Get("Content-Type"))
	}
	resp, body = send(t, c, "GET", "/files/2", "")
	if body != "<p>hi</p>" || resp.Header.Get("Content-Type") != "text/html" {
		t.Errorf("answered %q %q", body, resp.Header.Get("Content-Type"))
	}
}

func TestNoContentHasNoBody(t *testing.T) {
	b, c := newBackend(t, Options{})
	b.Reply("DELETE", "/orders/1", 204, map[string]any{"ignored": true})

	resp, body := send(t, c, "DELETE", "/orders/1", "")

	if resp.StatusCode != 204 || resp.Status != "204 No Content" || body != "" || resp.ContentLength != 0 {
		t.Errorf("answered %q %q %d", resp.Status, body, resp.ContentLength)
	}
	if got := resp.Header.Get("Content-Type"); got != "" {
		t.Errorf("content type = %q", got)
	}
}

func TestWhatNoDeclarationAnswersIs501AndAFailure(t *testing.T) {
	b, c := newBackend(t, Options{Name: "orders"})
	b.Reply("GET", "/orders/1", 200, nil)

	resp, body := send(t, c, "GET", "/orders/9?x=1", "")
	send(t, c, "POST", "/orders", "")

	if resp.StatusCode != 501 || resp.Header.Get("Content-Type") != "application/json" {
		t.Errorf("answered %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if body != `{"code":"unanswered","message":"GET /orders/9"}` {
		t.Errorf("body = %s", body)
	}
	want := []string{"orders was called where no step declared its answer: GET /orders/9?x=1, POST /orders"}
	if got := b.Failures(); !slices.Equal(got, want) {
		t.Errorf("failures = %q", got)
	}
}

func TestTheNeighbourIsNamedWhenNothingNamesIt(t *testing.T) {
	b, c := newBackend(t, Options{})
	send(t, c, "GET", "/x", "")

	want := []string{"the neighbour was called where no step declared its answer: GET /x"}
	if got := b.Failures(); !slices.Equal(got, want) {
		t.Errorf("failures = %q", got)
	}
}

func TestTimeOutFailsTheCallAsATimeout(t *testing.T) {
	b, c := newBackend(t, Options{Name: "orders"})
	b.Reply("GET", "/orders/1", 200, nil)
	b.Hold("GET", "/orders/1")
	b.TimeOut("GET", "/orders/1")

	_, err := c.Get(host + "/orders/1")

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want it to wrap context.DeadlineExceeded", err)
	}
	var failed *url.Error
	if !errors.As(err, &failed) || !failed.Timeout() {
		t.Errorf("err = %#v, want a *url.Error that Timeout()s", err)
	}
	if !strings.Contains(err.Error(), "orders did not answer GET /orders/1 in time") {
		t.Errorf("err = %v", err)
	}
	if got := len(b.Requests("GET", "/orders/1")); got != 1 {
		t.Errorf("%d requests kept, want 1", got)
	}
	if got := b.Failures(); len(got) != 0 {
		t.Errorf("failures = %q", got)
	}
}

func TestTimeOutTakesTheQueryItNames(t *testing.T) {
	b, c := newBackend(t, Options{})
	b.Reply("GET", "/orders", 200, nil)
	b.TimeOut("GET", "/orders", url.Values{"status": {"slow"}})

	if _, err := c.Get(host + "/orders?status=slow"); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("slow: %v", err)
	}
	if resp, _ := send(t, c, "GET", "/orders?status=fast", ""); resp.StatusCode != 200 {
		t.Errorf("fast answered %d", resp.StatusCode)
	}
}

func TestAHoldWaitsForItsRelease(t *testing.T) {
	b, c := newBackend(t, Options{})
	b.Reply("GET", "/orders/1", 200, "before")
	b.Hold("GET", "/orders/1")

	done := async(context.Background(), c, "GET", "/orders/1")
	eventually(t, "the request to arrive", func() bool { return len(b.Requests("GET", "/orders/1")) == 1 })
	select {
	case o := <-done:
		t.Fatalf("answered before the release: %+v", o)
	case <-time.After(50 * time.Millisecond):
	}

	b.Reply("GET", "/orders/1", 200, "after")
	if err := b.Release("GET", "/orders/1"); err != nil {
		t.Fatal(err)
	}

	if o := receive(t, done); o.err != nil || o.body != `"after"` {
		t.Errorf("released into %+v, want what is declared by then", o)
	}
	if _, body := send(t, c, "GET", "/orders/1", ""); body != `"after"` {
		t.Errorf("a request after the release answered %s", body)
	}
	if got := b.Failures(); len(got) != 0 {
		t.Errorf("failures = %q", got)
	}
}

func TestAHoldTakesTheQueryAndTheTemplateItNames(t *testing.T) {
	b, c := newBackend(t, Options{})
	b.Reply("GET", "/orders/{id}", 200, "order")
	b.Hold("GET", "/orders/{id}", url.Values{"status": {"open"}})

	if _, body := send(t, c, "GET", "/orders/1?status=closed", ""); body != `"order"` {
		t.Errorf("another query answered %s", body)
	}
	done := async(context.Background(), c, "GET", "/orders/2?status=open")
	eventually(t, "the request to arrive", func() bool { return len(b.Requests("GET", "/orders/2")) == 1 })

	if err := b.Release("GET", "/orders/{id}", url.Values{"status": {"open"}}); err != nil {
		t.Fatal(err)
	}
	if o := receive(t, done); o.err != nil || o.body != `"order"` {
		t.Errorf("released into %+v", o)
	}
}

func TestReleasingWhatNothingHeldIsAnError(t *testing.T) {
	b, _ := newBackend(t, Options{})

	if err := b.Release("GET", "/orders/1"); err == nil || err.Error() != "no step held GET /orders/1" {
		t.Errorf("err = %v", err)
	}

	b.Hold("GET", "/orders", url.Values{"status": {"open"}})
	if err := b.Release("GET", "/orders"); err == nil || err.Error() != "no step held GET /orders" {
		t.Errorf("another query: err = %v", err)
	}
	if err := b.Release("GET", "/orders", url.Values{"status": {"open"}}); err != nil {
		t.Errorf("err = %v", err)
	}
	if err := b.Release("GET", "/orders", url.Values{"status": {"open"}}); err == nil || err.Error() != "no step held GET /orders?status=open" {
		t.Errorf("twice: err = %v", err)
	}
}

func TestWhatNoStepReleasedIsAFailureUnlessTheCallerGaveUp(t *testing.T) {
	b, c := newBackend(t, Options{Name: "orders"})
	b.Hold("GET", "/orders/{id}")

	kept := async(context.Background(), c, "GET", "/orders/1?x=1")
	eventually(t, "the first request to arrive", func() bool { return len(b.Requests("GET", "/orders/{id}")) == 1 })
	ctx, cancel := context.WithCancel(context.Background())
	gone := async(ctx, c, "GET", "/orders/2")
	eventually(t, "the second request to arrive", func() bool { return len(b.Requests("GET", "/orders/{id}")) == 2 })

	want := []string{"orders holds what no step released: GET /orders/1?x=1, GET /orders/2"}
	if got := b.Failures(); !slices.Equal(got, want) {
		t.Errorf("failures = %q, want %q", got, want)
	}

	cancel()
	if o := receive(t, gone); !errors.Is(o.err, context.Canceled) {
		t.Errorf("the cancelled request: %+v", o)
	}
	want = []string{"orders holds what no step released: GET /orders/1?x=1"}
	if got := b.Failures(); !slices.Equal(got, want) {
		t.Errorf("failures = %q, want %q", got, want)
	}

	b.Reply("GET", "/orders/{id}", 200, nil)
	if err := b.Release("GET", "/orders/{id}"); err != nil {
		t.Fatal(err)
	}
	if o := receive(t, kept); o.err != nil || o.status != 200 {
		t.Errorf("the held request: %+v", o)
	}
	if got := b.Failures(); len(got) != 0 {
		t.Errorf("failures = %q", got)
	}
}
