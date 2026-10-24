// Package trace keeps what crossed a service's edge during a run, as the records
// flows/1 is written from. A stack's adapter records its transports through it.
package trace

import (
	"encoding/json"
	"reflect"
	"slices"
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
}

var (
	seq      atomic.Int64
	on       atomic.Bool
	mu       sync.Mutex
	kept     []Record
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
func Begin() int64 { return seq.Add(1) }

// Write keeps r under the scenario and step on now, in its place or the next one.
func Write(r Record) {
	mu.Lock()
	defer mu.Unlock()
	r.Service, r.Scenario, r.Feature, r.Step = service, scenario, feature, step
	if r.Seq == 0 {
		r.Seq = seq.Add(1)
	}
	kept = append(kept, r)
}

// Raw is body as a record holds it: JSON as it is, anything else quoted.
func Raw(body []byte) json.RawMessage {
	if len(body) == 0 {
		return nil
	}
	if json.Valid(body) {
		return body
	}
	quoted, _ := json.Marshal(body)
	return quoted
}
