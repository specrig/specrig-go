package trace

import (
	"context"
	"testing"

	"gitlab.com/gorib/amqp"
)

type discard struct{}

func (discard) Publish(context.Context, string, any) error { return nil }

// The outbox hands over the body it stored, already encoded.
func TestAnEncodedMessageIsRecordedAsTheBodyItCarries(t *testing.T) {
	Enable("shop")

	if err := Publisher(discard{}).Publish(context.Background(), "orders.paid", amqp.EncodedMessage(`{"id":7}`)); err != nil {
		t.Fatal(err)
	}

	records := Records()
	if got := string(records[len(records)-1].Request); got != `{"id":7}` {
		t.Errorf("recorded %s, want the body itself", got)
	}
}
