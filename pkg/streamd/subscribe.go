package streamd

import (
	"context"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
)

func autoResubscribe[T any](
	ctx context.Context,
	fn func(context.Context) (<-chan T, error),
) (<-chan T, <-chan struct{}, error) {
	input, err := fn(ctx)
	if err != nil {
		return nil, nil, err
	}
	result := make(chan T, 1)
	restartCh := make(chan struct{}, 1)
	observability.Go(ctx, func(ctx context.Context) {
		defer func() {
			var sample T
			logger.Debugf(ctx, "autoResubscribe[%T] handler is closed", sample)
		}()
		defer close(result)
		defer close(restartCh)
		for {
			for {
				var (
					ev T
					ok bool
				)
				select {
				case <-ctx.Done():
					return
				case ev, ok = <-input:
				}
				if !ok {
					logger.Debugf(ctx, "the input channel is closed; reconnect")
					break
				}
				select {
				case <-ctx.Done():
					return
				case result <- ev:
				}
			}
			for {
				input, err = fn(ctx)
				if err == nil {
					break
				}
				logger.Warnf(ctx, "unable to reconnect: %w")
				time.Sleep(time.Second)
			}
			restartCh <- struct{}{}
		}
	})
	return result, restartCh, nil
}
