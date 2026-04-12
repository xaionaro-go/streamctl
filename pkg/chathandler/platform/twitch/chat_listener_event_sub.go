package twitch

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/nicklaw5/helix/v2"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
)

// EventSubListener implements chathandler.ChatListener using Twitch EventSub WebSocket.
// Uses pre-authorized tokens from streamd -- no interactive OAuth required.
type EventSubListener struct {
	ClientID     string
	ClientSecret string
	AccessToken  string
	RefreshToken string
	ChannelID    string
	handler      *twitch.ChatHandlerSub
}

func (l *EventSubListener) Name() string { return "EventSub" }

func (l *EventSubListener) Listen(
	ctx context.Context,
) (_ <-chan streamcontrol.Event, _err error) {
	logger.Tracef(ctx, "EventSubListener.Listen")
	defer func() { logger.Tracef(ctx, "/EventSubListener.Listen: %v", _err) }()

	client, err := helix.NewClientWithContext(ctx, &helix.Options{
		ClientID:     l.ClientID,
		ClientSecret: l.ClientSecret,
	})
	if err != nil {
		return nil, fmt.Errorf("create helix client: %w", err)
	}

	client.SetUserAccessToken(l.AccessToken)
	if l.RefreshToken != "" {
		client.SetRefreshToken(l.RefreshToken)
	}

	userID, err := twitch.GetUserID(ctx, client, l.ChannelID)
	if err != nil {
		return nil, fmt.Errorf("get user ID for %q: %w", l.ChannelID, err)
	}

	h, err := twitch.NewChatHandlerSub(ctx, client, userID, nil)
	if err != nil {
		return nil, fmt.Errorf("create EventSub handler: %w", err)
	}
	l.handler = h

	return h.MessagesChan(), nil
}

func (l *EventSubListener) Close(ctx context.Context) error {
	if l.handler != nil {
		return l.handler.Close(ctx)
	}
	return nil
}
