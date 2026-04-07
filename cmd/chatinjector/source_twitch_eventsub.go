package main

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/nicklaw5/helix/v2"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	twitchpkg "github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	twitch "github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch/types"
)

// TwitchEventSubSource implements ChatSource using Twitch EventSub WebSocket.
// It requires OAuth credentials (client_id and access_token) and provides
// richer event support than IRC (subscriptions, cheers, raids, etc.).
type TwitchEventSubSource struct {
	// Channel is the Twitch channel name to monitor.
	Channel string
	// ClientID is the Twitch application client ID.
	ClientID string
	// AccessToken is the Twitch user access token.
	AccessToken string
}

func (s *TwitchEventSubSource) PlatformID() streamcontrol.PlatformName {
	return twitch.ID
}

// Run creates a helix client, resolves the broadcaster ID, and subscribes
// to EventSub events, forwarding them as ChatEvents until the context is
// cancelled or an unrecoverable error occurs.
func (s *TwitchEventSubSource) Run(
	ctx context.Context,
	events chan<- ChatEvent,
) (_err error) {
	logger.Tracef(ctx, "TwitchEventSubSource.Run")
	defer func() { logger.Tracef(ctx, "/TwitchEventSubSource.Run: %v", _err) }()

	switch {
	case s.Channel == "":
		return fmt.Errorf("twitch channel is required")
	case s.ClientID == "":
		return fmt.Errorf("twitch client_id is required for eventsub source")
	case s.AccessToken == "":
		return fmt.Errorf("twitch access_token is required for eventsub source")
	}

	client, err := helix.NewClientWithContext(ctx, &helix.Options{
		ClientID:        s.ClientID,
		UserAccessToken: s.AccessToken,
	})
	if err != nil {
		return fmt.Errorf("create helix client: %w", err)
	}

	broadcasterID, err := twitchpkg.GetUserID(ctx, client, s.Channel)
	if err != nil {
		return fmt.Errorf("resolve broadcaster ID for channel %q: %w", s.Channel, err)
	}
	logger.Debugf(ctx, "resolved channel %q to broadcaster ID %s", s.Channel, broadcasterID)

	handler, err := twitchpkg.NewChatHandlerSub(ctx, client, broadcasterID, nil)
	if err != nil {
		return fmt.Errorf("create EventSub handler for channel %q: %w", s.Channel, err)
	}
	defer handler.Close(ctx)

	logger.Debugf(ctx, "connected to Twitch EventSub for channel %q", s.Channel)

	msgCh := handler.MessagesChan()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-msgCh:
			if !ok {
				logger.Debugf(ctx, "Twitch EventSub message channel closed")
				return nil
			}
			logger.Debugf(ctx, "received twitch eventsub event: id=%s type=%s user=%s msg=%q",
				ev.ID, ev.Type, ev.User.Name, messageContent(ev))

			select {
			case events <- ChatEvent{Event: ev, Platform: twitch.ID}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}
