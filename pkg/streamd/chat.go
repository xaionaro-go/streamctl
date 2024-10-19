package streamd

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/events"
)

func (d *StreamD) startListeningForChatMessages(
	ctx context.Context,
	platName streamcontrol.PlatformName,
) error {
	ctrl, err := d.streamController(ctx, platName)
	if err != nil {
		return fmt.Errorf("unable to get the just initialized '%s': %w", platName, err)
	}
	ch, err := ctrl.GetChatMessagesChan(ctx)
	observability.Go(ctx, func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-ch:
				err := d.notifyAboutChatMessage(ctx, api.ChatMessage{
					ChatMessage: ev,
					Platform:    platName,
				})
				if err != nil {
					logger.Errorf(ctx, "unable to notify about chat message %#+v: %w", ev, err)
				}
			}
		}
	})
	return nil
}

func (d *StreamD) notifyAboutChatMessage(
	ctx context.Context,
	ev api.ChatMessage,
) error {
	logger.Debugf(ctx, "notifyAboutChatMessage(ctx, %#+v)", ev)
	defer logger.Debugf(ctx, "/notifyAboutChatMessage(ctx, %#+v)", ev)
	d.EventBus.Publish(events.ChatMessage, ev)
	return nil
}
