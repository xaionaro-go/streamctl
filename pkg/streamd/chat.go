package streamd

import (
	"context"
	"fmt"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
)

func (d *StreamD) startListeningForChatMessages(
	ctx context.Context,
	platName streamcontrol.PlatformName,
) error {
	logger.Debugf(ctx, "startListeningForChatMessages(ctx, '%s')", platName)
	ctrl, err := d.streamController(ctx, platName)
	if err != nil {
		return fmt.Errorf("unable to get the just initialized '%s': %w", platName, err)
	}
	ch, err := ctrl.GetChatMessagesChan(ctx)
	if err != nil {
		return fmt.Errorf("unable to get the channel for chat messages of '%s': %w", platName, err)
	}
	observability.Go(ctx, func() {
		logger.Debugf(ctx, "/startListeningForChatMessages(ctx, '%s')", platName)
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				d.publishEvent(ctx, api.ChatMessage{
					ChatMessage: ev,
					Platform:    platName,
				})
			}
		}
	})
	return nil
}

func (d *StreamD) RemoveChatMessage(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	msgID streamcontrol.ChatMessageID,
) error {
	ctrl, err := d.streamController(ctx, platID)
	if err != nil {
		return fmt.Errorf("unable to get stream controller '%s': %w", platID, err)
	}

	err = ctrl.RemoveChatMessage(ctx, msgID)
	if err != nil {
		return fmt.Errorf("unable to remove message '%s' on '%s': %w", msgID, platID, err)
	}

	return nil
}

func (d *StreamD) BanUser(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	userID streamcontrol.ChatUserID,
	reason string,
	deadline time.Time,
) error {
	ctrl, err := d.streamController(ctx, platID)
	if err != nil {
		return fmt.Errorf("unable to get stream controller '%s': %w", platID, err)
	}

	err = ctrl.BanUser(ctx, streamcontrol.ChatUserID(userID), reason, deadline)
	if err != nil {
		return fmt.Errorf("unable to ban user '%s' on '%s': %w", userID, platID, err)
	}

	return nil
}
