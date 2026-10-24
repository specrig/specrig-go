// Shaped like protoc-gen-go-grpc output, without its dependencies.
package pb

import "context"

const (
	OrderService_Place_FullMethodName  = "/order.OrderService/Place"
	OrderService_Cancel_FullMethodName = "/order.OrderService/Cancel"
	StockService_Reserve_FullMethodName = "/stock.StockService/Reserve"
)

type Request struct{}
type Response struct{}

type OrderServiceServer interface {
	Place(context.Context, *Request) (*Response, error)
	Cancel(context.Context, *Request) (*Response, error)
}

type UnimplementedOrderServiceServer struct{}

func (UnimplementedOrderServiceServer) Place(context.Context, *Request) (*Response, error)  { return nil, nil }
func (UnimplementedOrderServiceServer) Cancel(context.Context, *Request) (*Response, error) { return nil, nil }

func RegisterOrderServiceServer(s any, srv OrderServiceServer) {}

type StockServiceClient interface {
	Reserve(ctx context.Context, in *Request, opts ...any) (*Response, error)
}
