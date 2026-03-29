package streamd

import (
	"context"
	"errors"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
)

func (d *StreamD) ListenChatOfStream(
	ctx context.Context,
	platID streamcontrol.PlatformID,
	streamID streamcontrol.StreamID,
) (_ <-chan api.ChatMessage, _err error) {
	logger.Debugf(ctx, "ListenChatOfStream(ctx, '%s', '%s')", platID, streamID)
	defer func() { logger.Debugf(ctx, "/ListenChatOfStream(ctx, '%s', '%s'): %v", platID, streamID, _err) }()

	ctrls, err := d.StreamControllers(ctx, platID)
	if err != nil {
		return nil, fmt.Errorf("unable to get stream controllers for platform '%s': %w", platID, err)
	}

	var errs []error
	for _, ctrl := range ctrls {
		evCh, err := ctrl.GetChatMessagesChan(ctx, streamID)
		if err != nil {
			errs = append(errs, fmt.Errorf("controller %s: %w", ctrl, err))
			continue
		}

		outCh := make(chan api.ChatMessage, 1000)
		observability.Go(ctx, func(ctx context.Context) {
			defer close(outCh)
			for {
				select {
				case <-ctx.Done():
					logger.Debugf(ctx, "ListenChatOfStream goroutine: context done; %v", ctx.Err())
					return
				case ev, ok := <-evCh:
					if !ok {
						logger.Debugf(ctx, "ListenChatOfStream goroutine: event channel closed")
						return
					}
					msg := api.ChatMessage{
						Event:    ev,
						IsLive:   true,
						Platform: platID,
						StreamID: streamID,
					}
					if err := d.ChatMessagesStorage.AddMessage(ctx, msg); err != nil {
						logger.Errorf(ctx, "unable to add message to storage: %v", err)
					}
					publishEvent(ctx, d.EventBus, msg)
					select {
					case <-ctx.Done():
						return
					case outCh <- msg:
					}
				}
			}
		})
		return outCh, nil
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("unable to listen to stream '%s' on platform '%s': %w", streamID, platID, errors.Join(errs...))
	}
	return nil, fmt.Errorf("no controller for platform '%s' found", platID)
}
