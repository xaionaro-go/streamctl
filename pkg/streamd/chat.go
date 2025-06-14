package streamd

import (
	"context"
	"fmt"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
)

type ChatMessageStorage interface {
	AddMessage(context.Context, api.ChatMessage) error
	RemoveMessage(context.Context, streamcontrol.ChatMessageID) error
	Load(ctx context.Context) error
	Store(ctx context.Context) error
	GetMessagesSince(context.Context, time.Time) ([]api.ChatMessage, error)
}

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
		defer logger.Debugf(ctx, "/startListeningForChatMessages(ctx, '%s')", platName)
		for {
			select {
			case <-ctx.Done():
				logger.Debugf(ctx, "startListeningForChatMessages(ctx, '%s'): context is closed; %v", platName, ctx.Err())
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				msg := api.ChatMessage{
					ChatMessage: ev,
					Platform:    platName,
				}
				if err := d.ChatMessagesStorage.AddMessage(ctx, msg); err != nil {
					logger.Errorf(ctx, "unable to add the message %#+v to the chat messages storage: %v", msg, err)
				}
				d.publishEvent(ctx, msg)
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

	if err := d.ChatMessagesStorage.RemoveMessage(ctx, msgID); err != nil {
		logger.Errorf(ctx, "unable to remove the message from the chat messages storage: %v", err)
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

func (d *StreamD) SubscribeToChatMessages(
	ctx context.Context,
	since time.Time,
) (<-chan api.ChatMessage, error) {
	return eventSubToChan[api.ChatMessage](
		ctx, d,
		func(ctx context.Context, outCh chan api.ChatMessage) {
			msgs, err := d.ChatMessagesStorage.GetMessagesSince(ctx, since)
			if err != nil {
				logger.Errorf(ctx, "unable to get the messages from the storage: %v", err)
				return
			}
			for _, msg := range msgs {
				outCh <- msg
			}
		},
	)
}

func (d *StreamD) SendChatMessage(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	message string,
) (_err error) {
	logger.Debugf(ctx, "SendChatMessage(ctx, '%s', '%s')", platID, message)
	defer func() { logger.Debugf(ctx, "/SendChatMessage(ctx, '%s', '%s'): %v", platID, message, _err) }()
	if message == "" {
		return nil
	}

	ctrl, err := d.streamController(ctx, platID)
	if err != nil {
		return fmt.Errorf("unable to get stream controller for platform '%s': %w", platID, err)
	}

	err = ctrl.SendChatMessage(ctx, message)
	if err != nil {
		return fmt.Errorf("unable to send message '%s' to platform '%s': %w", message, platID, err)
	}

	return nil
}
