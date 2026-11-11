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

func TestReplyWithAnswersFromTheRequest(t *testing.T) {
	b, c := newBackend(t, Options{})
	b.ReplyWith("POST", "/orders/{id}", func(r Received) (int, any) {
		var in struct{ Sku string }
		if err := r.JSON(&in); err != nil {
			return 400, err.Error()
		}
		return 201, map[string]any{"id": strings.TrimPrefix(r.Path, "/orders/"), "sku": in.Sku}
	}, Header("X-Answer", "computed"))

	for id, sku := range map[string]string{"1": "a", "2": "b"} {
		resp, body := send(t, c, "POST", "/orders/"+id, `{"sku":"`+sku+`"}`)
		if resp.StatusCode != 201 || body != `{"id":"`+id+`","sku":"`+sku+`"}` {
			t.Errorf("order %s answered %d %s", id, resp.StatusCode, body)
		}
		if resp.Header.Get("Content-Type") != "application/json" || resp.Header.Get("X-Answer") != "computed" {
			t.Errorf("order %s: header = %v", id, resp.Header)
		}
	}
}

func TestAComputedAnswerIsEncodedAsADeclaredOne(t *testing.T) {
	b, c := newBackend(t, Options{})
	b.ReplyWith("GET", "/empty", func(Received) (int, any) { return 200, nil })
	b.ReplyWith("GET", "/mixed", func(r Received) (int, any) {
		if r.Query.Has("raw") {
			return 200, []byte("raw")
		}
		return 200, "json"
	})
	b.ReplyWith("GET", "/xml", func(Received) (int, any) { return 200, []byte("<a/>") }, Header("Content-Type", "application/xml"))
	b.ReplyWith("DELETE", "/orders/1", func(Received) (int, any) { return 204, "ignored" })

	for _, want := range []struct{ target, body, contentType string }{
		{"/empty", "{}", "application/json"},
		{"/mixed?raw", "raw", "application/octet-stream"},
		{"/mixed", `"json"`, "application/json"},
		{"/xml", "<a/>", "application/xml"},
	} {
		if resp, body := send(t, c, "GET", want.target, ""); body != want.body || resp.Header.Get("Content-Type") != want.contentType {
			t.Errorf("%s answered %q %q", want.target, body, resp.Header.Get("Content-Type"))
		}
	}
	resp, body := send(t, c, "DELETE", "/orders/1", "")
	if resp.StatusCode != 204 || body != "" || resp.Header.Get("Content-Type") != "" {
		t.Errorf("answered %d %q %q", resp.StatusCode, body, resp.Header.Get("Content-Type"))
	}
}

func TestAComputedAnswerMayReadWhatReachedTheBackend(t *testing.T) {
	b, c := newBackend(t, Options{})
	b.ReplyWith("GET", "/session", func(r Received) (int, any) {
		if len(b.Requests("GET", "/session")) == 1 {
			return 401, "expired"
		}
		return 200, r.Header.Get("Authorization")
	})

	if resp, _ := send(t, c, "GET", "/session", ""); resp.StatusCode != 401 {
		t.Errorf("the first call answered %d", resp.StatusCode)
	}
	req, err := http.NewRequest("GET", host+"/session", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer fresh")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if raw, _ := io.ReadAll(resp.Body); resp.StatusCode != 200 || string(raw) != `"Bearer fresh"` {
		t.Errorf("the second call answered %d %s", resp.StatusCode, raw)
	}
}

func TestAComputedAnswerIsADeclarationNamingNoQuery(t *testing.T) {
	b, c := newBackend(t, Options{})
	b.ReplyWith("GET", "/orders", func(Received) (int, any) { return 200, "computed" })
	b.ReplyTo("GET", "/orders", url.Values{"status": {"open"}}, 200, "open")

	if _, body := send(t, c, "GET", "/orders?status=closed", ""); body != `"computed"` {
		t.Errorf("closed answered %s", body)
	}
	if _, body := send(t, c, "GET", "/orders?status=open", ""); body != `"open"` {
		t.Errorf("open answered %s", body)
	}
	b.Reply("GET", "/orders", 200, "declared")
	if _, body := send(t, c, "GET", "/orders", ""); body != `"declared"` {
		t.Errorf("answered %s after Reply", body)
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

func TestHealLiftsEveryTimeOutAndKeepsTheReplies(t *testing.T) {
	b, c := newBackend(t, Options{})
	b.Reply("GET", "/orders", 200, "orders")
	b.Reply("GET", "/stock", 200, "stock")
	b.TimeOut("GET", "/orders")
	b.TimeOut("GET", "/stock", url.Values{"sku": {"a"}})
	b.Reply("GET", "/orders", 200, "orders again")

	if _, err := c.Get(host + "/orders"); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a reply lifted the time-out: %v", err)
	}
	b.Heal()

	if _, body := send(t, c, "GET", "/orders", ""); body != `"orders again"` {
		t.Errorf("orders answered %s", body)
	}
	if _, body := send(t, c, "GET", "/stock?sku=a", ""); body != `"stock"` {
		t.Errorf("stock answered %s", body)
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

func TestResetForgetsTheScenario(t *testing.T) {
	b, c := newBackend(t, Options{Name: "orders"})
	b.Reply("GET", "/orders/1", 200, nil)
	b.TimeOut("GET", "/slow")
	b.Hold("GET", "/held")
	send(t, c, "GET", "/orders/9", "")
	held := async(context.Background(), c, "GET", "/held")
	eventually(t, "the held request to arrive", func() bool { return len(b.Requests("GET", "/held")) == 1 })

	b.Reset()

	if o := receive(t, held); o.err == nil || !strings.Contains(o.err.Error(), "orders: the scenario ended before a step released GET /held") {
		t.Errorf("the held request: %+v", o)
	}
	if got := b.Failures(); len(got) != 0 {
		t.Errorf("failures = %q", got)
	}
	if got := b.Requests("GET", "/orders/9"); len(got) != 0 {
		t.Errorf("requests = %v", got)
	}
	for _, target := range []string{"/orders/1", "/slow", "/held"} {
		if resp, _ := send(t, c, "GET", target, ""); resp.StatusCode != 501 {
			t.Errorf("%s answered %d after the reset", target, resp.StatusCode)
		}
	}
}
