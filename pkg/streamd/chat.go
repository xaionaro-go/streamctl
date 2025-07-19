package streamd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
)

const (
	debugSendArchiveMessagesAsLive = false
)

type ChatMessageStorage interface {
	AddMessage(context.Context, api.ChatMessage) error
	RemoveMessage(context.Context, streamcontrol.ChatMessageID) error
	Load(ctx context.Context) error
	Store(ctx context.Context) error
	GetMessagesSince(context.Context, time.Time, uint) ([]api.ChatMessage, error)
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
	observability.Go(ctx, func(ctx context.Context) {
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
				func() {
					msg := api.ChatMessage{
						ChatMessage: ev,
						IsLive:      true,
						Platform:    platName,
					}
					logger.Tracef(ctx, "received chat message: %#+v", msg)
					defer logger.Tracef(ctx, "finished processing the chat message")
					if err := d.ChatMessagesStorage.AddMessage(ctx, msg); err != nil {
						logger.Errorf(ctx, "unable to add the message %#+v to the chat messages storage: %v", msg, err)
					}
					publishEvent(ctx, d.EventBus, msg)
					d.shoutoutIfNeeded(ctx, msg)
				}()
			}
		}
	})
	return nil
}

func (d *StreamD) shoutoutIfNeeded(
	ctx context.Context,
	msg api.ChatMessage,
) {
	logger.Tracef(ctx, "shoutoutIfNeeded(ctx, %#+v)", msg)
	defer logger.Tracef(ctx, "/shoutoutIfNeeded(ctx, %#+v)", msg)
	if !msg.IsLive {
		logger.Tracef(ctx, "is not a live message")
		return
	}

	d.lastShoutoutAtLocker.Lock()
	defer d.lastShoutoutAtLocker.Unlock()

	userID := config.ChatUserID{
		Platform: msg.Platform,
		User:     streamcontrol.ChatUserID(strings.ToLower(string(msg.UserID))),
	}
	lastShoutoutAt := d.lastShoutoutAt[userID]
	logger.Tracef(ctx, "lastShoutoutAt(%#+v): %v", userID, lastShoutoutAt)
	if v := time.Since(lastShoutoutAt); v < time.Hour {
		logger.Tracef(ctx, "the previous shoutout was too soon: %v < %v", v, time.Hour)
		return
	}

	cfg, err := d.GetConfig(ctx)
	if err != nil {
		logger.Errorf(ctx, "unable to get the config: %v", err)
		return
	}

	found := false
	for _, _candidate := range cfg.Shoutout.AutoShoutoutOnMessage {
		if _candidate.Platform != msg.Platform {
			continue
		}
		candidate := config.ChatUserID{
			Platform: _candidate.Platform,
			User:     streamcontrol.ChatUserID(strings.ToLower(string(_candidate.User))),
		}
		if candidate == userID {
			found = true
			break
		}
	}

	if !found {
		logger.Tracef(ctx, "not in the list for auto-shoutout")
		return
	}

	d.shoutoutIfCan(ctx, userID.Platform, userID.User)
}

func (d *StreamD) shoutoutIfCan(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	userID streamcontrol.ChatUserID,
) {
	logger.Tracef(ctx, "shoutoutIfCan('%s', '%s')", platID, userID)
	defer logger.Tracef(ctx, "/shoutoutIfCan('%s', '%s')", platID, userID)

	ctrl, err := d.streamController(ctx, platID)
	if err != nil {
		logger.Errorf(ctx, "unable to get a stream controller '%s': %v", platID, err)
		return
	}

	if !ctrl.IsCapable(ctx, streamcontrol.CapabilityShoutout) {
		logger.Errorf(ctx, "the controller '%s' does not support shoutouts", platID)
		return
	}

	err = ctrl.Shoutout(ctx, userID)
	if err != nil {
		logger.Errorf(ctx, "unable to shoutout '%s' at '%s': %v", userID, platID, err)
		return
	}
	userFullID := config.ChatUserID{
		Platform: platID,
		User:     userID,
	}
	d.lastShoutoutAt[userFullID] = time.Now()
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
	limit uint64,
) (_ret <-chan api.ChatMessage, _err error) {
	logger.Tracef(ctx, "SubscribeToChatMessages(ctx, %v, %v)", since, limit)
	defer func() { logger.Tracef(ctx, "/SubscribeToChatMessages(ctx, %v, %v): %p %v", since, limit, _ret, _err) }()

	return eventSubToChan(
		ctx, d.EventBus, 1000,
		func(ctx context.Context, outCh chan api.ChatMessage) {
			logger.Tracef(ctx, "backfilling the channel")
			defer func() { logger.Tracef(ctx, "/backfilling the channel") }()
			msgs, err := d.ChatMessagesStorage.GetMessagesSince(ctx, since, uint(limit))
			if err != nil {
				logger.Errorf(ctx, "unable to get the messages from the storage: %v", err)
				return
			}
			for _, msg := range msgs {
				msg.IsLive = false
				if debugSendArchiveMessagesAsLive {
					msg.IsLive = true
				}
				if !func() (_ret bool) {
					defer func() {
						if recover() != nil {
							logger.Debugf(ctx, "the channel is closed")
							_ret = false
						}
					}()
					outCh <- msg
					return true
				}() {
					break
				}
				if debugSendArchiveMessagesAsLive {
					time.Sleep(5 * time.Second)
				}
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
