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
	RemoveMessage(context.Context, streamcontrol.EventID) error
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

	listenerCtx, cancel := context.WithCancel(ctx)
	d.chatListenerLocker.Lock()
	if oldCancel := d.chatListenerCancels[platName]; oldCancel != nil {
		oldCancel()
	}
	d.chatListenerCancels[platName] = cancel
	d.chatListenerLocker.Unlock()

	observability.Go(ctx, func(_ context.Context) {
		defer logger.Debugf(listenerCtx, "/startListeningForChatMessages(ctx, '%s')", platName)
		for {
			select {
			case <-listenerCtx.Done():
				logger.Debugf(ctx, "chat listener for '%s' stopped: %v", platName, listenerCtx.Err())
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				func() {
					msg := api.ChatMessage{
						Event:    ev,
						IsLive:   true,
						Platform: platName,
					}
					if err := d.processChatMessage(ctx, msg); err != nil {
						logger.Errorf(ctx, "unable to process the chat message %#+v: %v", msg, err)
					}
				}()
			}
		}
	})
	return nil
}

func (d *StreamD) processChatMessage(
	ctx context.Context,
	msg api.ChatMessage,
) error {
	logger.Tracef(ctx, "processChatMessage")
	defer logger.Tracef(ctx, "/processChatMessage")

	if err := d.ChatMessagesStorage.AddMessage(ctx, msg); err != nil {
		logger.Errorf(ctx, "unable to add the message to the chat messages storage: %v", err)
	}

	publishEvent(ctx, d.EventBus, msg)
	d.shoutoutIfNeeded(ctx, msg)
	return nil
}

func (d *StreamD) InjectChatMessage(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	ev streamcontrol.Event,
) (_err error) {
	logger.Debugf(ctx, "InjectChatMessage")
	defer func() { logger.Debugf(ctx, "/InjectChatMessage: %v", _err) }()

	msg := api.ChatMessage{
		Event:    ev,
		IsLive:   true,
		Platform: platID,
	}
	return d.processChatMessage(ctx, msg)
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
	platID streamcontrol.PlatformName,
	userID streamcontrol.UserID,
) (_ret bool) {
	logger.Debugf(ctx, "shoutoutIfCan('%s', '%s')", platID, userID)
	defer logger.Debugf(ctx, "/shoutoutIfCan('%s', '%s')", platID, userID)

	ctrl, err := d.streamController(ctx, platID)
	if err != nil {
		logger.Errorf(ctx, "unable to get a stream controller '%s': %v", platID, err)
		return false
	}

	if !ctrl.IsCapable(ctx, streamcontrol.CapabilityShoutout) {
		logger.Errorf(ctx, "the controller '%s' does not support shoutouts", platID)
		return false
	}

	err = ctrl.Shoutout(ctx, userID)
	if err != nil {
		logger.Errorf(ctx, "unable to shoutout '%s' at '%s': %v", userID, platID, err)
		return false
	}
	userFullID := config.ChatUserID{
		Platform: platID,
		User:     userID,
	}
	d.lastShoutoutAt[userFullID] = time.Now()
	return true
}

func (d *StreamD) RemoveChatMessage(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	msgID streamcontrol.EventID,
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
	userID streamcontrol.UserID,
	reason string,
	deadline time.Time,
) error {
	ctrl, err := d.streamController(ctx, platID)
	if err != nil {
		return fmt.Errorf("unable to get stream controller '%s': %w", platID, err)
	}

	err = ctrl.BanUser(ctx, streamcontrol.UserID(userID), reason, deadline)
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

func (d *StreamD) SetBuiltinChatListenerEnabled(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	enabled bool,
) (_err error) {
	logger.Debugf(ctx, "SetBuiltinChatListenerEnabled(ctx, '%s', %v)", platID, enabled)
	defer func() { logger.Debugf(ctx, "/SetBuiltinChatListenerEnabled: %v", _err) }()

	d.chatListenerLocker.Lock()
	cancel := d.chatListenerCancels[platID]
	d.chatListenerLocker.Unlock()

	switch {
	case !enabled && cancel != nil:
		// Stop the listener — cancels context, which stops YouTube API polling.
		cancel()
		d.chatListenerLocker.Lock()
		delete(d.chatListenerCancels, platID)
		d.chatListenerLocker.Unlock()
		logger.Debugf(ctx, "stopped chat listener for '%s'", platID)
	case enabled && cancel == nil:
		// Restart the listener.
		if err := d.startListeningForChatMessages(ctx, platID); err != nil {
			return fmt.Errorf("restart chat listener for '%s': %w", platID, err)
		}
		logger.Debugf(ctx, "restarted chat listener for '%s'", platID)
	}

	return nil
}

func (d *StreamD) IsBuiltinChatListenerEnabled(
	ctx context.Context,
	platID streamcontrol.PlatformName,
) (bool, error) {
	logger.Debugf(ctx, "IsBuiltinChatListenerEnabled(ctx, '%s')", platID)

	d.chatListenerLocker.Lock()
	defer d.chatListenerLocker.Unlock()
	return d.chatListenerCancels[platID] != nil, nil
}
