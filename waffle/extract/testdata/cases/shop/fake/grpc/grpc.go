package grpc

import "context"

type CallOption interface{}

type ServiceDesc struct{ ServiceName string }

type ServiceRegistrar interface {
	RegisterService(desc *ServiceDesc, impl any)
}

type ClientConn struct{}

func (*ClientConn) Invoke(ctx context.Context, method string, args, reply any, opts ...CallOption) error {
	return nil
}
