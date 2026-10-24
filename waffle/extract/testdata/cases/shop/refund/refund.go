// Written without waffle's pieces: a bare channel, a bare connection, a bare
// registration.
package refund

import (
	"context"

	amqp "github.com/rabbitmq/amqp091-go"
	"google.golang.org/grpc"

	"example.com/shop/pb"
)

type bus struct{ ch *amqp.Channel }

func (b *bus) send(ctx context.Context, key string) error {
	return b.ch.PublishWithContext(ctx, "events", key, false, false, amqp.Publishing{})
}

func (b *bus) listen() error { return b.ch.QueueBind("refunds", "payments.refunded", "events", false, nil) }

type service struct {
	bus   *bus
	audit *grpc.ClientConn
}

func (s *service) Issue(ctx context.Context) error {
	if err := s.audit.Invoke(ctx, "/audit.AuditService/Record", nil, nil); err != nil {
		return err
	}
	return s.bus.send(ctx, "refunds.issued")
}

func (s *service) register(r grpc.ServiceRegistrar) { r.RegisterService(&pb.RefundService_ServiceDesc, s) }
