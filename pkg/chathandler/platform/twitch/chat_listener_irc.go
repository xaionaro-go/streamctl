package twitch

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
)

// IRCListener implements chathandler.ChatListener using Twitch IRC (anonymous, no auth needed).
type IRCListener struct {
	ChannelID string
	handler   *twitch.ChatHandlerIRC
}

func (l *IRCListener) Name() string { return "IRC" }

func (l *IRCListener) Listen(
	ctx context.Context,
) (_ <-chan streamcontrol.Event, _err error) {
	logger.Tracef(ctx, "IRCListener.Listen")
	defer func() { logger.Tracef(ctx, "/IRCListener.Listen: %v", _err) }()

	h, err := twitch.NewChatHandlerIRC(ctx, l.ChannelID)
	if err != nil {
		return nil, fmt.Errorf("create IRC handler: %w", err)
	}
	l.handler = h

	return h.MessagesChan(), nil
}

func (l *IRCListener) Close(ctx context.Context) error {
	if l.handler != nil {
		return l.handler.Close(ctx)
	}
	return nil
}
