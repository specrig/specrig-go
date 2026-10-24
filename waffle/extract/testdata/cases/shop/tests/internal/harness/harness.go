// The suite publishes to drive the service; that is not the service's surface.
package harness

import "context"

type Publisher interface {
	Enqueue(ctx context.Context, subject string, document any) error
}

func Drive(ctx context.Context, p Publisher) error { return p.Enqueue(ctx, "tests.only", nil) }
