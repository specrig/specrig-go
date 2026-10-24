package client

import "context"

type RequestOption func()

type Response struct{}

type Client struct{}

func (*Client) Get(ctx context.Context, uri string, opts ...RequestOption) (*Response, error) {
	return nil, nil
}

func (*Client) Post(ctx context.Context, uri string, opts ...RequestOption) (*Response, error) {
	return nil, nil
}
