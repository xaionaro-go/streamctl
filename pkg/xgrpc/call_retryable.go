package xgrpc

import (
	"context"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"google.golang.org/grpc"
)

type RetryConfig struct {
	InitialInterval    time.Duration
	MaximalInterval    time.Duration
	IntervalMultiplier float64
}

type ClientRetryable[CT any] interface {
	Client[CT]
	GetRetryConfig() RetryConfig
}

func CallRetryable[REQ any, REPLY any, CT any, C ClientRetryable[CT]](
	ctx context.Context,
	c C,
	fn func(context.Context, *REQ, ...grpc.CallOption) (REPLY, error),
	req *REQ,
	opts ...grpc.CallOption,
) (REPLY, error) {
	var reply REPLY
	retryConfig := c.GetRetryConfig()
	callFn := func(ctx context.Context, opts ...grpc.CallOption) error {
		var err error
		delay := retryConfig.InitialInterval
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			reply, err = fn(ctx, req, opts...)
			if err == nil {
				return nil
			}
			err = c.ProcessError(ctx, err)
			if err != nil {
				return err
			}
			logger.Debugf(
				ctx,
				"retrying; sleeping %v for the retry",
				delay,
			)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			delay = min(time.Duration(
				float64(
					delay,
				)*retryConfig.IntervalMultiplier,
			), retryConfig.MaximalInterval)
		}
	}

	wrapper := c.GetCallWrapper()
	if wrapper == nil {
		err := callFn(ctx, opts...)
		return reply, err
	}

	err := wrapper(ctx, req, callFn, opts...)
	return reply, err
}
