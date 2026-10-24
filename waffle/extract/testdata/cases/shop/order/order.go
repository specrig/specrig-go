package order

import (
	"context"

	"example.com/shop/pb"
)

type Type string

const (
	Placed   Type = "orders.placed"
	Canceled Type = "orders.canceled"
	Created       = "orders.created"
	Updated       = "orders.updated"
)

type Publisher interface {
	Enqueue(ctx context.Context, subject string, document any) error
}

type service struct {
	pb.OrderServiceServer
	publisher Publisher
	stock     pb.StockServiceClient
}

// Serves Place itself; Cancel comes from the embedded interface and is not served.
func (s *service) Place(ctx context.Context, _ *pb.Request) (*pb.Response, error) {
	if _, err := s.stock.Reserve(ctx, nil); err != nil {
		return nil, err
	}
	if err := s.save(ctx, true); err != nil {
		return nil, err
	}
	return nil, s.publish(ctx, Placed)
}

func (s *service) cancel(ctx context.Context) error { return s.publish(ctx, Canceled) }

func (s *service) publish(ctx context.Context, event Type) error {
	return s.publisher.Enqueue(ctx, string(event), nil)
}

func (s *service) save(ctx context.Context, fresh bool) error {
	key := Updated
	if fresh {
		key = Created
	}
	return s.publisher.Enqueue(ctx, key, nil)
}

func (s *service) register(server any) { pb.RegisterOrderServiceServer(server, s) }

type paid struct{}

func (paid) Keys() []string { return []string{"payments.succeeded", "stock.#"} }
