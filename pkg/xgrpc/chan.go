package xgrpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"google.golang.org/grpc"
)

type Receiver[T any] interface {
	grpc.ClientStream

	Recv() (*T, error)
}

func UnwrapChan[E any, R any, S Receiver[R], CT any, C Client[CT]](
	ctx context.Context,
	c C,
	fn func(ctx context.Context, client CT) (S, error),
	parse func(ctx context.Context, event *R) E,
) (<-chan E, error) {

	pc, _, _, _ := runtime.Caller(1)
	caller := runtime.FuncForPC(pc)
	ctx = belt.WithField(ctx, "caller_func", caller.Name())

	ctx, cancelFn := context.WithCancel(ctx)
	getSub := func() (S, io.Closer, error) {
		client, closer, err := c.GRPCClient(ctx)
		if err != nil {
			var emptyS S
			return emptyS, nil, err
		}
		sub, err := fn(ctx, client)
		if err != nil {
			var emptyS S
			return emptyS, nil, fmt.Errorf(
				"unable to subscribe: %w",
				err,
			)
		}
		return sub, closer, nil
	}

	sub, closer, err := getSub()
	if err != nil {
		cancelFn()
		return nil, err
	}

	r := make(chan E)
	observability.Go(ctx, func(ctx context.Context) {
		defer closer.Close()
		defer cancelFn()
		for {
			event, err := sub.Recv()
			select {
			case <-ctx.Done():
				return
			default:
			}
			if err != nil {
				switch {
				case errors.Is(err, io.EOF):
					logger.Debugf(
						ctx,
						"the receiver is closed: %v",
						err,
					)
					return
				case strings.Contains(err.Error(), grpc.ErrClientConnClosing.Error()):
					logger.Debugf(
						ctx,
						"apparently we are closing the client: %v",
						err,
					)
					return
				case strings.Contains(err.Error(), context.Canceled.Error()):
					logger.Debugf(
						ctx,
						"subscription was cancelled: %v",
						err,
					)
					return
				default:
					for {
						err = c.ProcessError(ctx, err)
						if err != nil {
							logger.Errorf(
								ctx,
								"unable to read data: %v",
								err,
							)
							return
						}
						closer.Close()
						sub, closer, err = getSub()
						if err != nil {
							logger.Errorf(
								ctx,
								"unable to resubscribe: %v",
								err,
							)
							continue
						}
						break
					}
					continue
				}
			}

			r <- parse(ctx, event)
		}
	})
	return r, nil
}

type Sender[T any] interface {
	grpc.ServerStream

	Send(*T) error
}

func WrapChan[T any, E any](
	ctx context.Context,
	getChan func(ctx context.Context) (<-chan E, error),
	sender Sender[T],
	parse func(E) T,
) (_err error) {
	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()

	var tSample T
	var eSample E
	logger.Tracef(ctx, "WrapChan[%T, %T]", tSample, eSample)
	defer func() { logger.Tracef(ctx, "/WrapChan[%T, %T]: %v", tSample, eSample, _err) }()

	ch, err := getChan(ctx)
	if err != nil {
		return err
	}
	errCh := make(chan error, 1)
	for {
		var input E
		var ok bool
		select {
		case <-ctx.Done():
			return ctx.Err()
		case input, ok = <-ch:
		}
		if !ok {
			return fmt.Errorf("channel is closed")
		}
		result := parse(input)
		sendCtx, cancelFn := context.WithTimeout(ctx, time.Minute)
		observability.Go(ctx, func(ctx context.Context) {
			errCh <- sender.Send(&result)
		})
		select {
		case <-sendCtx.Done():
			logger.Warnf(ctx, "sending timed out")
			cancelFn()
			return sendCtx.Err()
		case err := <-errCh:
			cancelFn()
			if err != nil {
				return fmt.Errorf(
					"unable to send %#+v: %w",
					result,
					err,
				)
			}
		}
	}
}
