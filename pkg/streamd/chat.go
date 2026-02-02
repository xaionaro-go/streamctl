package streamd

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/clock"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
)

const (
	debugSendArchiveMessagesAsLive = false
)

type ChatMessageStorage interface {
	AddMessage(context.Context, api.ChatMessage) error
	RemoveMessage(context.Context, streamcontrol.EventID) error
	Load(ctx context.Context) error
	Store(ctx context.Context) error
	GetMessagesSince(context.Context, time.Time, uint) ([]api.ChatMessage, error)
}

func (d *StreamD) startListeningForChatMessages(
	ctx context.Context,
	platID streamcontrol.PlatformID,
	accountID streamcontrol.AccountID,
	ctrl streamcontrol.AbstractAccount,
) {
	logger.Debugf(ctx, "startListeningForChatMessages(ctx, %s, %s)", platID, accountID)
	observability.Go(ctx, func(ctx context.Context) {
		defer logger.Debugf(ctx, "/startListeningForChatMessages(ctx, %s, %s)", platID, accountID)
		ch, err := ctrl.GetChatMessagesChan(ctx, streamcontrol.DefaultStreamID)
		if err != nil {
			logger.Errorf(ctx, "unable to get the channel for chat messages of '%s:%s': %v", platID, accountID, err)
			return
		}

		d.processChatMessages(ctx, platID, accountID, ch)
	})
}

func (d *StreamD) processChatMessages(
	ctx context.Context,
	platID streamcontrol.PlatformID,
	accountID streamcontrol.AccountID,
	ch <-chan streamcontrol.Event,
) {
	for {
		select {
		case <-ctx.Done():
			logger.Debugf(ctx, "processChatMessages(ctx, '%s', '%s'): context is closed; %v", platID, accountID, ctx.Err())
			return
		case ev, ok := <-ch:
			if !ok {
				logger.Debugf(ctx, "processChatMessages(ctx, '%s', '%s'): the channel is closed", platID, accountID)
				return
			}
			func() {
				msg := api.ChatMessage{
					Event:    ev,
					IsLive:   true,
					Platform: platID,
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
}

func (d *StreamD) shoutoutIfNeeded(
	ctx context.Context,
	msg api.ChatMessage,
) (_ret bool) {
	logger.Debugf(ctx, "shoutoutIfNeeded(ctx, %#+v)", msg)
	defer logger.Debugf(ctx, "/shoutoutIfNeeded(ctx, %#+v): %v", msg, _ret)
	if !msg.IsLive {
		logger.Tracef(ctx, "is not a live message")
		return false
	}

	d.lastShoutoutAtLocker.Lock()
	defer d.lastShoutoutAtLocker.Unlock()

	userID := config.ChatUserID{
		Platform: msg.Platform,
		User:     streamcontrol.UserID(strings.ToLower(string(msg.User.ID))),
	}
	userIDByName := config.ChatUserID{
		Platform: msg.Platform,
		User:     streamcontrol.UserID(strings.ToLower(string(msg.User.Name))),
	}
	lastShoutoutAt := d.lastShoutoutAt[userID]
	logger.Debugf(ctx, "lastShoutoutAt(%#+v): %v", userID, lastShoutoutAt)
	if v := time.Since(lastShoutoutAt); v < time.Hour {
		logger.Tracef(ctx, "the previous shoutout was too soon: %v < %v", v, time.Hour)
		return false
	}

	cfg, err := d.GetConfig(ctx)
	if err != nil {
		logger.Errorf(ctx, "unable to get the config: %v", err)
		return false
	}

	found := false
	for _, _candidate := range cfg.Shoutout.AutoShoutoutOnMessage {
		if _candidate.Platform != msg.Platform {
			continue
		}
		candidate := config.ChatUserID{
			Platform: _candidate.Platform,
			User:     streamcontrol.UserID(strings.ToLower(string(_candidate.User))),
		}
		if candidate == userID {
			found = true
			break
		}
		if candidate == userIDByName {
			found = true
			break
		}
	}

	if !found {
		logger.Debugf(ctx, "'%s' not in the list for auto-shoutout at '%s'", userID.User, msg.Platform)
		return false
	}

	return d.shoutoutIfCan(ctx, userID.Platform, userID.User)
}

func (d *StreamD) shoutoutIfCan(
	ctx context.Context,
	platID streamcontrol.PlatformID,
	userID streamcontrol.UserID,
) (_ret bool) {
	logger.Debugf(ctx, "shoutoutIfCan('%s', '%s')", platID, userID)
	defer logger.Debugf(ctx, "/shoutoutIfCan('%s', '%s')", platID, userID)

	ctrls, err := d.StreamControllers(ctx, platID)
	if err != nil {
		logger.Errorf(ctx, "unable to get stream controllers '%s': %v", platID, err)
		return false
	}

	anySuccess := false
	for _, ctrl := range ctrls {
		if !ctrl.IsCapable(ctx, streamcontrol.CapabilityShoutout) {
			continue
		}

		err = ctrl.Shoutout(ctx, streamcontrol.DefaultStreamID, userID)
		if err != nil {
			logger.Errorf(ctx, "unable to shoutout '%s' at '%s': %v", userID, platID, err)
			continue
		}
		anySuccess = true
	}

	if anySuccess {
		userFullID := config.ChatUserID{
			Platform: platID,
			User:     userID,
		}
		d.lastShoutoutAt[userFullID] = clock.Get().Now()
	}
	return anySuccess
}

func (d *StreamD) RemoveChatMessage(
	ctx context.Context,
	platID streamcontrol.PlatformID,
	msgID streamcontrol.EventID,
) error {
	ctrls, err := d.StreamControllers(ctx, platID)
	if err != nil {
		return fmt.Errorf("unable to get stream controllers '%s': %w", platID, err)
	}

	for _, ctrl := range ctrls {
		err = ctrl.RemoveChatMessage(ctx, streamcontrol.DefaultStreamID, msgID)
		if err != nil {
			logger.Errorf(ctx, "unable to remove message '%s' on '%s': %v", msgID, platID, err)
		}
	}

	if err := d.ChatMessagesStorage.RemoveMessage(ctx, msgID); err != nil {
		logger.Errorf(ctx, "unable to remove the message from the chat messages storage: %v", err)
	}

	return nil
}

// InjectPlatformEvent forcefully injects a fake chat message into the system for debugging purposes.
// The message will be added to the chat storage and published on the EventBus but won't be
// forwarded to external platform controllers.
func (d *StreamD) InjectPlatformEvent(
	ctx context.Context,
	platID streamcontrol.PlatformID,
	isLive bool,
	isPersistent bool,
	user streamcontrol.User,
	message string,
) (_err error) {
	logger.Debugf(ctx, "InjectPlatformEvent(ctx, '%s', %#+v, '%s')", platID, user, message)
	defer func() {
		logger.Debugf(ctx, "/InjectPlatformEvent(ctx, '%s', %#+v, '%s'): %v", platID, user, message, _err)
	}()

	msg := api.ChatMessage{
		Event: streamcontrol.Event{
			ID: streamcontrol.EventID(fmt.Sprintf("injected-%d-%d",
				clock.Get().Now().UnixNano(),
				rand.Uint64(),
			)),
			CreatedAt: clock.Get().Now(),
			Type:      streamcontrol.EventTypeChatMessage,
			User:      user,
			Message: &streamcontrol.Message{
				Content: message,
				Format:  streamcontrol.TextFormatTypePlain,
			},
		},
		IsLive:   isLive,
		Platform: platID,
	}

	if isPersistent {
		logger.Debug(ctx, "storing injected message to storage")
		if err := d.ChatMessagesStorage.AddMessage(ctx, msg); err != nil {
			logger.Errorf(ctx, "unable to add injected message to storage: %v", err)
		}
	}

	// publish via EventBus so subscribers receive it
	publishEvent(ctx, d.EventBus, msg)

	// Try shoutout side-effects if configured
	d.shoutoutIfNeeded(ctx, msg)

	return nil
}

func (d *StreamD) BanUser(
	ctx context.Context,
	platID streamcontrol.PlatformID,
	userID streamcontrol.UserID,
	reason string,
	deadline time.Time,
) error {
	ctrls, err := d.StreamControllers(ctx, platID)
	if err != nil {
		return fmt.Errorf("unable to get stream controllers '%s': %w", platID, err)
	}

	for _, ctrl := range ctrls {
		err = ctrl.BanUser(ctx, streamcontrol.DefaultStreamID, streamcontrol.UserID(userID), reason, deadline)
		if err != nil {
			logger.Errorf(ctx, "unable to ban user '%s' on '%s': %v", userID, platID, err)
		}
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
					clock.Get().Sleep(5 * time.Second)
				}
			}
		},
	)
}

func (d *StreamD) SendChatMessage(
	ctx context.Context,
	platID streamcontrol.PlatformID,
	message string,
) (_err error) {
	logger.Debugf(ctx, "SendChatMessage(ctx, '%s', '%s')", platID, message)
	defer func() { logger.Debugf(ctx, "/SendChatMessage(ctx, '%s', '%s'): %v", platID, message, _err) }()
	if message == "" {
		return nil
	}

	ctrls, err := d.StreamControllers(ctx, platID)
	if err != nil {
		return fmt.Errorf("unable to get stream controllers for platform '%s': %w", platID, err)
	}

	for _, ctrl := range ctrls {
		err = ctrl.SendChatMessage(ctx, streamcontrol.DefaultStreamID, message)
		if err != nil {
			logger.Errorf(ctx, "unable to send message '%s' to platform '%s': %v", message, platID, err)
		}
	}

	return nil
}
