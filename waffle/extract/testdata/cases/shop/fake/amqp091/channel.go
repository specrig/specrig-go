package amqp091

import "context"

type Publishing struct{}
type Table map[string]any

type Channel struct{}

func (*Channel) PublishWithContext(ctx context.Context, exchange, key string, mandatory, immediate bool, msg Publishing) error {
	return nil
}

func (*Channel) QueueBind(name, key, exchange string, noWait bool, args Table) error { return nil }
