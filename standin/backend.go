// Package standin is the stand-in for a service's HTTP neighbour: it answers
// what the steps declared, checks the traffic against the neighbour's OpenAPI
// document and keeps what was sent.
package standin

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/specrig/specrig-go/trace"
)

type Options struct {
	Name     string // how failures name the neighbour
	Contract string // its OpenAPI 3 document, a path as the suite's working directory reaches it; empty means no check
	Failure  string // a components.schemas name: the body a 5xx may carry on any route
}

type Backend struct {
	name     string
	file     string
	contract *document
	failure  *openapi3.SchemaRef

	mu         sync.Mutex
	replies    table[*reply]
	timeouts   table[struct{}]
	holds      table[*hold]
	received   []Received
	unanswered []string
	violations []string
}

type Received struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   []byte
}

type reply struct {
	status int
	body   []byte
	header http.Header
}

type Option func(*reply)

func Header(name, value string) Option {
	return func(r *reply) { r.header.Add(name, value) }
}

type hold struct {
	waiting []*waiting
}

type waiting struct {
	call     string
	ctx      context.Context
	released chan struct{}
}

type call struct {
	req    *http.Request
	method string
	path   string
	query  url.Values
	body   []byte
	route  *found
	answer []byte
}

func New(o Options) (*Backend, error) {
	b := &Backend{name: cmp.Or(o.Name, "the neighbour"), file: o.Contract}
	if o.Contract == "" {
		return b, nil
	}
	doc, err := load(o.Contract)
	if err != nil {
		return nil, fmt.Errorf("%s's contract %s: %w", b.name, o.Contract, err)
	}
	b.contract = doc
	if o.Failure != "" {
		if b.failure = doc.schema(o.Failure); b.failure == nil {
			return nil, fmt.Errorf("%s's contract %s has no schema %s", b.name, o.Contract, o.Failure)
		}
	}
	return b, nil
}

func (b *Backend) Reply(method, path string, status int, body any, opts ...Option) {
	b.ReplyTo(method, path, nil, status, body, opts...)
}

func (b *Backend) ReplyTo(method, path string, query url.Values, status int, body any, opts ...Option) {
	r := &reply{status: status, header: http.Header{}}
	for _, o := range opts {
		o(r)
	}
	if status != http.StatusNoContent {
		contentType := "application/json"
		switch v := body.(type) {
		case nil:
			r.body = []byte("{}")
		case []byte:
			r.body, contentType = slices.Clone(v), "application/octet-stream"
		default:
			encoded, err := json.Marshal(v)
			if err != nil {
				panic(fmt.Sprintf("standin: the body of %s %s cannot be encoded: %v", method, path, err))
			}
			r.body = encoded
		}
		if r.header.Get("Content-Type") == "" {
			r.header.Set("Content-Type", contentType)
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.replies.set(method+" "+path, query, r)
}

func (b *Backend) TimeOut(method, path string, query ...url.Values) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.timeouts.set(method+" "+path, first(query), struct{}{})
}

func (b *Backend) Hold(method, path string, query ...url.Values) {
	key, q := method+" "+path, first(query)
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, held := b.holds.get(key, q); !held {
		b.holds.set(key, q, &hold{})
	}
}

func (b *Backend) Release(method, path string, query ...url.Values) error {
	key, q := method+" "+path, first(query)
	b.mu.Lock()
	h, held := b.holds.remove(key, q)
	var queued []*waiting
	if held {
		queued = h.waiting
	}
	b.mu.Unlock()
	if !held {
		return fmt.Errorf("no step held %s", describe(key, q))
	}
	for _, w := range queued {
		close(w.released)
	}
	return nil
}

func (b *Backend) Requests(method, path string) []Received {
	b.mu.Lock()
	defer b.mu.Unlock()
	var found []Received
	for _, r := range b.received {
		if r.Method == method && takes(path, r.Path) {
			found = append(found, r)
		}
	}
	return found
}

func (b *Backend) Failures() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var failures []string
	if len(b.unanswered) > 0 {
		failures = append(failures, fmt.Sprintf("%s was called where no step declared its answer: %s", b.name, strings.Join(b.unanswered, ", ")))
	}
	var unreleased []string
	for _, key := range b.holds.keys {
		for _, d := range b.holds.entries[key] {
			for _, w := range d.value.waiting {
				// A request the service gave up on has nobody left to answer.
				if w.ctx.Err() == nil {
					unreleased = append(unreleased, w.call)
				}
			}
		}
	}
	if len(unreleased) > 0 {
		failures = append(failures, fmt.Sprintf("%s holds what no step released: %s", b.name, strings.Join(unreleased, ", ")))
	}
	if len(b.violations) > 0 {
		failures = append(failures, fmt.Sprintf("what %s's contract, %s, does not allow: %s", b.name, b.file, strings.Join(b.violations, "; ")))
	}
	return failures
}

func (b *Backend) RoundTrip(req *http.Request) (*http.Response, error) {
	c := &call{req: req, method: cmp.Or(req.Method, http.MethodGet), path: req.URL.Path, query: req.URL.Query()}
	c.route = b.contract.find(c.method, c.path)
	if trace.On() {
		seq := trace.Begin()
		defer func() {
			trace.Write(trace.Record{Seq: seq, Dir: "out", Kind: "http", Name: c.name(), Request: trace.Raw(c.body), Response: trace.Raw(c.answer)})
		}()
	}
	if req.Body != nil {
		var err error
		c.body, err = io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: reading the request body: %w", b.name, err)
		}
	}

	violation := b.checkRequest(c)
	var w *waiting
	var timedOut bool
	b.mu.Lock()
	b.received = append(b.received, Received{Method: c.method, Path: c.path, Query: c.query, Header: req.Header.Clone(), Body: c.body})
	if violation != "" {
		b.violations = append(b.violations, violation)
	}
	if _, timedOut = b.timeouts.find(c.method, c.path, c.query); !timedOut {
		if h, held := b.holds.find(c.method, c.path, c.query); held {
			w = &waiting{call: c.line(), ctx: req.Context(), released: make(chan struct{})}
			h.waiting = append(h.waiting, w)
		}
	}
	b.mu.Unlock()

	if timedOut {
		return nil, &timeoutError{fmt.Sprintf("%s did not answer %s %s in time", b.name, c.method, c.path)}
	}
	if w != nil {
		select {
		case <-w.released:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}
	return b.fulfil(c), nil
}

func (b *Backend) fulfil(c *call) *http.Response {
	b.mu.Lock()
	r, answered := b.replies.find(c.method, c.path, c.query)
	if !answered {
		b.unanswered = append(b.unanswered, c.line())
	}
	b.mu.Unlock()

	if !answered {
		body, _ := json.Marshal(struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}{"unanswered", c.method + " " + c.path})
		return c.respond(&reply{status: http.StatusNotImplemented, body: body, header: http.Header{"Content-Type": {"application/json"}}})
	}
	if b.contract != nil && c.route != nil {
		if msg := b.contract.response(c.route, c.req, r.status, r.header, r.body, b.failure); msg != "" {
			b.mu.Lock()
			b.violations = append(b.violations, fmt.Sprintf("%s %s answered %d: %s", c.method, c.path, r.status, msg))
			b.mu.Unlock()
		}
	}
	return c.respond(r)
}

func (b *Backend) checkRequest(c *call) string {
	switch {
	case b.contract == nil:
		return ""
	case c.route == nil:
		return fmt.Sprintf("%s %s: %s serves no such route", c.method, c.path, b.name)
	}
	if msg := b.contract.request(c.route, c.req, c.body); msg != "" {
		return fmt.Sprintf("%s %s: %s", c.method, c.path, msg)
	}
	return ""
}

// Under the servers' base path, as the neighbour serves it: the name has to meet its input.
func (c *call) name() string {
	if c.route != nil {
		return c.method + " " + strings.TrimSuffix(c.route.base, "/") + c.route.Path
	}
	return c.method + " " + c.path
}

func (c *call) line() string {
	if c.req.URL.RawQuery == "" {
		return c.method + " " + c.path
	}
	return c.method + " " + c.path + "?" + c.req.URL.RawQuery
}

func (c *call) respond(r *reply) *http.Response {
	c.answer = r.body
	var body io.ReadCloser = http.NoBody
	if len(r.body) > 0 {
		body = io.NopCloser(bytes.NewReader(r.body))
	}
	return &http.Response{
		Status:        strings.TrimSpace(fmt.Sprintf("%d %s", r.status, http.StatusText(r.status))),
		StatusCode:    r.status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        r.header.Clone(),
		Body:          body,
		ContentLength: int64(len(r.body)),
		Request:       c.req,
	}
}

// http.Client reports a timeout only when the error says so itself, which fmt's wrapper does not.
type timeoutError struct{ msg string }

func (e *timeoutError) Error() string { return e.msg }
func (e *timeoutError) Timeout() bool { return true }
func (e *timeoutError) Unwrap() error { return context.DeadlineExceeded }

func (r Received) JSON(into any) error {
	if len(r.Body) == 0 {
		return fmt.Errorf("%s %s carried no body", r.Method, r.Path)
	}
	return json.Unmarshal(r.Body, into)
}

type declared[T any] struct {
	query url.Values
	value T
}

type table[T any] struct {
	keys    []string
	entries map[string][]declared[T]
}

func (t *table[T]) set(key string, query url.Values, value T) {
	if t.entries == nil {
		t.entries = map[string][]declared[T]{}
	}
	list, known := t.entries[key]
	if !known {
		t.keys = append(t.keys, key)
	}
	list = slices.DeleteFunc(list, func(d declared[T]) bool { return sameQuery(d.query, query) })
	t.entries[key] = append(list, declared[T]{query, value})
}

func (t *table[T]) get(key string, query url.Values) (value T, ok bool) {
	for _, d := range t.entries[key] {
		if sameQuery(d.query, query) {
			return d.value, true
		}
	}
	return value, false
}

func (t *table[T]) remove(key string, query url.Values) (value T, ok bool) {
	list := t.entries[key]
	at := slices.IndexFunc(list, func(d declared[T]) bool { return sameQuery(d.query, query) })
	if at < 0 {
		return value, false
	}
	value = list[at].value
	t.entries[key] = slices.Delete(list, at, at+1)
	return value, true
}

// The exact path is taken before any template, even when none of its declarations takes the query.
func (t *table[T]) find(method, path string, query url.Values) (value T, ok bool) {
	if exact := t.entries[method+" "+path]; len(exact) > 0 {
		return pick(exact, query)
	}
	prefix := method + " "
	for _, key := range t.keys {
		if strings.HasPrefix(key, prefix) && len(t.entries[key]) > 0 && takes(key[len(prefix):], path) {
			return pick(t.entries[key], query)
		}
	}
	return value, false
}

// >=, so that of two declarations naming as many keys the later wins.
func pick[T any](candidates []declared[T], query url.Values) (value T, ok bool) {
	named := -1
	for _, c := range candidates {
		if len(c.query) >= named && matches(c.query, query) {
			value, ok, named = c.value, true, len(c.query)
		}
	}
	return value, ok
}

func matches(wanted, got url.Values) bool {
	for name, values := range wanted {
		have := got[name]
		if len(have) == 0 || (len(values) == 1 && have[0] != values[0]) || (len(values) != 1 && !slices.Equal(have, values)) {
			return false
		}
	}
	return true
}

func sameQuery(a, b url.Values) bool {
	return maps.EqualFunc(a, b, slices.Equal[[]string])
}

func first(query []url.Values) url.Values {
	if len(query) == 0 {
		return nil
	}
	return query[0]
}

func describe(key string, query url.Values) string {
	if len(query) == 0 {
		return key
	}
	return key + "?" + query.Encode()
}

func takes(template, path string) bool {
	_, ok := bind(template, path)
	return ok
}

func bind(template, path string) (params map[string]string, ok bool) {
	segments, wanted := strings.Split(template, "/"), strings.Split(path, "/")
	if len(segments) != len(wanted) {
		return nil, false
	}
	for i, s := range segments {
		switch {
		case len(s) >= 2 && s[0] == '{' && s[len(s)-1] == '}':
			if wanted[i] == "" {
				return nil, false
			}
			if params == nil {
				params = map[string]string{}
			}
			params[s[1:len(s)-1]] = wanted[i]
		case s != wanted[i]:
			return nil, false
		}
	}
	return params, true
}
