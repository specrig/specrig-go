package outbox

import "context"

type Publisher interface {
	Publish(ctx context.Context, subject string, document any) error
	Enqueue(ctx context.Context, subject string, document any) error
}
