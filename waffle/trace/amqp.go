package trace

import (
	"context"
	"encoding/json"
	"io"

	"gitlab.com/gorib/amqp"

	record "github.com/specrig/specrig-go/trace"
)

type publishing interface {
	Publish(ctx context.Context, key string, document any) error
}

// Publisher records what p publishes. A service may narrow the publisher to its
// own interface without Close; the one returned closes p when p can be closed.
func Publisher(p publishing) amqp.Publisher { return publisher{p} }

type publisher struct{ publishing }

func (p publisher) Publish(ctx context.Context, key string, document any) error {
	err := p.publishing.Publish(ctx, key, document)
	if err == nil && record.On() {
		record.Event("out", key, encoded(document))
	}
	return err
}

func (p publisher) Close() error {
	if c, ok := p.publishing.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

func encoded(document any) []byte {
	switch d := document.(type) {
	case []byte:
		return d
	case json.RawMessage:
		return d
	case amqp.EncodedMessage:
		return d
	}
	body, _ := json.Marshal(document)
	return body
}
