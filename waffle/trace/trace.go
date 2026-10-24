// Package trace records what crosses a waffle service's edge in a suite — its
// gRPC and HTTP calls, its commands, the messages waffle's tools move — into the
// records flows/1 is written from.
package trace

import (
	record "github.com/specrig/specrig-go/trace"
	"gitlab.com/gorib/waffle/tools/tap"
)

type Record = record.Record

// Enable starts keeping records for the suite of svc, the messages outbox and
// the scribus recorder move among them.
func Enable(svc string) {
	record.Enable(svc)
	tap.Listen(record.Event)
}

func Case(uri string) { record.Case(uri) }

func Step(kind string) { record.Step(kind) }

func Start(t interface{ Name() string }) { record.Start(t) }

func Records() []Record { return record.Records() }
