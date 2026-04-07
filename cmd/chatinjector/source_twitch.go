package main

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	twitchpkg "github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	twitch "github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch/types"
)

// TwitchSource implements ChatSource using Twitch IRC (anonymous, read-only).
// It connects to the public IRC endpoint and joins the specified channel,
// forwarding chat messages as ChatEvents.
//
// For richer event support (subscriptions, cheers, raids, etc.), a future
// enhancement could use EventSub via NewChatHandlerSub, which requires
// OAuth credentials (client_id, client_secret, access_token).
type TwitchSource struct {
	// Channel is the Twitch channel name to join (e.g. "xqc").
	Channel string
}

func (s *TwitchSource) PlatformID() streamcontrol.PlatformName {
	return twitch.ID
}

// Run connects to Twitch IRC, joins the configured channel, and emits
// ChatEvents until the context is cancelled or an unrecoverable error occurs.
func (s *TwitchSource) Run(
	ctx context.Context,
	events chan<- ChatEvent,
) (_err error) {
	logger.Tracef(ctx, "TwitchSource.Run")
	defer func() { logger.Tracef(ctx, "/TwitchSource.Run: %v", _err) }()

	if s.Channel == "" {
		return fmt.Errorf("twitch channel is required")
	}

	handler, err := twitchpkg.NewChatHandlerIRC(ctx, s.Channel)
	if err != nil {
		return fmt.Errorf("create Twitch IRC handler for channel %q: %w", s.Channel, err)
	}
	defer handler.Close(ctx)

	logger.Debugf(ctx, "connected to Twitch IRC channel %q", s.Channel)

	msgCh := handler.MessagesChan()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-msgCh:
			if !ok {
				logger.Debugf(ctx, "Twitch IRC message channel closed")
				return nil
			}
			logger.Debugf(ctx, "received twitch event: id=%s type=%s user=%s msg=%q",
				ev.ID, ev.Type, ev.User.Name, messageContent(ev))

			select {
			case events <- ChatEvent{Event: ev, Platform: twitch.ID}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}
