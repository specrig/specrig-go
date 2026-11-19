// Package trace keeps what crossed a service's edge during a run, as the records
// flows/2 is written from. A stack's adapter records its transports through it.
package trace

import (
	"bytes"
	"encoding/json"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
)

type Record struct {
	// A call is written once it returns, after whatever it sent on the way, so
	// the order it began in is kept here.
	Seq      int64
	Service  string
	Scenario string
	Feature  string // the file the scenario is in, as the suite named it
	Step     string // Context, Action or Outcome: Given, When, Then
	Dir      string // in, out
	Kind     string // grpc, event, http, command
	Name     string // the full method, the routing key, the route or the command
	Request  json.RawMessage
	Response json.RawMessage
	Error    string // the refusal by code and failure type
	// The goroutine a call was made on, then the one that started it, and on as
	// far as is known: goroutines started side by side call in no fixed order.
	Threads []uint64
}

var (
	seq      atomic.Int64
	on       atomic.Bool
	mu       sync.Mutex
	kept     []Record
	begun    = map[int64]Record{}
	service  string
	scenario string
	feature  string
	step     string
)

// Case names the file of the scenario about to run.
func Case(uri string) {
	mu.Lock()
	defer mu.Unlock()
	feature = uri
}

// Step marks what the scenario's current step is for. Start leaves it alone: a
// scenario boots inside its first step.
func Step(kind string) {
	mu.Lock()
	defer mu.Unlock()
	step = kind
}

// Enable starts keeping records for the suite of svc.
func Enable(svc string) {
	mu.Lock()
	defer mu.Unlock()
	service = svc
	on.Store(true)
}

// A suite runs its scenarios one after another, so the one being booted is the
// one every record belongs to until the next boot.
func Start(t interface{ Name() string }) {
	mu.Lock()
	defer mu.Unlock()
	scenario = ""
	// godog hands out a nil pointer outside a scenario, typed, so not a nil interface.
	if t != nil && (reflect.ValueOf(t).Kind() != reflect.Pointer || !reflect.ValueOf(t).IsNil()) {
		scenario = t.Name()
	}
}

// Records is everything kept so far, in the order it was written.
func Records() []Record {
	mu.Lock()
	defer mu.Unlock()
	return slices.Clone(kept)
}

// Event records a message: "in" for a delivery, "out" for a publication.
func Event(dir, key string, body []byte) {
	if !on.Load() {
		return
	}
	Write(Record{Dir: dir, Kind: "event", Name: key, Request: Raw(body)})
}

// On is whether records are being kept.
func On() bool { return on.Load() }

// Begin takes the next place in the order, for a call that writes its record
// once it returns.
func Begin() int64 {
	n := seq.Add(1)
	if on.Load() {
		r := Record{Threads: threads()}
		mu.Lock()
		begun[n] = r
		mu.Unlock()
	}
	return n
}

// Write keeps r under the scenario and step on now, in its place or the next one.
func Write(r Record) {
	var chain []uint64
	if on.Load() {
		chain = threads()
	}
	mu.Lock()
	defer mu.Unlock()
	r.Service, r.Scenario, r.Feature, r.Step = service, scenario, feature, step
	r.Threads = chain
	if r.Seq == 0 {
		r.Seq = seq.Add(1)
	} else if b, ok := begun[r.Seq]; ok {
		r.Threads = b.Threads
		delete(begun, r.Seq)
	}
	kept = append(kept, r)
}

var (
	lineage sync.Mutex
	parents = map[uint64]uint64{} // 0 for a goroutine whose parent is not known
)

// A goroutine's own stack names only its parent; one that has made no call yet is
// looked up among every goroutine alive, and is lost once it has ended.
func threads() []uint64 {
	id, parent := created(stack(false))
	lineage.Lock()
	defer lineage.Unlock()
	parents[id] = parent
	chain := []uint64{id}
	for at := id; ; {
		p, known := parents[at]
		if !known {
			for _, block := range bytes.Split(stack(true), []byte("\n\n")) {
				g, p := created(block)
				parents[g] = p
			}
			if p, known = parents[at]; !known {
				parents[at] = 0
			}
		}
		if p == 0 {
			return chain
		}
		chain = append(chain, p)
		at = p
	}
}

func stack(all bool) []byte {
	buf := make([]byte, 1<<12)
	for {
		n := runtime.Stack(buf, all)
		if n < len(buf) {
			return buf[:n]
		}
		buf = make([]byte, 2*len(buf))
	}
}

// A goroutine's stack opens with "goroutine 9 [" and ends with "created by f in
// goroutine 7".
func created(stack []byte) (id, parent uint64) {
	head, _, _ := bytes.Cut(bytes.TrimPrefix(stack, []byte("goroutine ")), []byte(" "))
	id, _ = strconv.ParseUint(string(head), 10, 64)
	_, by, ok := bytes.Cut(stack, []byte("\ncreated by "))
	if !ok {
		return id, 0
	}
	line, _, _ := bytes.Cut(by, []byte("\n"))
	if i := bytes.LastIndex(line, []byte(" in goroutine ")); i >= 0 {
		parent, _ = strconv.ParseUint(string(line[i+len(" in goroutine "):]), 10, 64)
	}
	return id, parent
}

// Raw is body as a record holds it: JSON as it is, anything else quoted.
func Raw(body []byte) json.RawMessage {
	if len(body) == 0 {
		return nil
	}
	if json.Valid(body) {
		return body
	}
	quoted, _ := json.Marshal(string(body))
	return quoted
}
