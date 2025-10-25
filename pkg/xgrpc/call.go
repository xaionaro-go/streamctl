package xgrpc

import (
	"context"
	"io"

	"google.golang.org/grpc"
)

type CallWrapperFunc func(
	ctx context.Context,
	req any,
	callFunc func(ctx context.Context, opts ...grpc.CallOption) error,
	opts ...grpc.CallOption,
) error

type Client[CT any] interface {
	GRPCClient(context.Context) (CT, io.Closer, error)
	ProcessError(ctx context.Context, err error) error
	GetCallWrapper() CallWrapperFunc
}

func Call[REQ any, REPLY any, CT any, C Client[CT]](
	ctx context.Context,
	c C,
	fn func(context.Context, *REQ, ...grpc.CallOption) (REPLY, error),
	req *REQ,
	opts ...grpc.CallOption,
) (REPLY, error) {
	var reply REPLY
	callFn := func(ctx context.Context, opts ...grpc.CallOption) error {
		var err error
		reply, err = fn(ctx, req, opts...)
		if err == nil {
			return nil
		}
		err = c.ProcessError(ctx, err)
		if err != nil {
			return err
		}
		return nil
	}

	wrapper := c.GetCallWrapper()
	if wrapper == nil {
		err := callFn(ctx, opts...)
		return reply, err
	}

	err := wrapper(ctx, req, callFn, opts...)
	return reply, err
}
