package kick

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	chatwebhookclient "github.com/xaionaro-go/chatwebhook/pkg/grpc/client"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
)

// WebSocketListener implements chathandler.ChatListener using Kick WebSocket via chatwebhook.
type WebSocketListener struct {
	ChatWebhookAddr string
	handler         *kick.ChatHandler
}

func (l *WebSocketListener) Name() string { return "WebSocket" }

func (l *WebSocketListener) Listen(
	ctx context.Context,
) (_ <-chan streamcontrol.Event, _err error) {
	logger.Tracef(ctx, "WebSocketListener.Listen")
	defer func() { logger.Tracef(ctx, "/WebSocketListener.Listen: %v", _err) }()

	c, err := chatwebhookclient.New(ctx, l.ChatWebhookAddr)
	if err != nil {
		return nil, fmt.Errorf("connect to chatwebhook at %s: %w", l.ChatWebhookAddr, err)
	}

	h, err := kick.NewChatHandler(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("create Kick chat handler: %w", err)
	}
	l.handler = h

	ch, err := h.GetMessagesChan(ctx)
	if err != nil {
		return nil, fmt.Errorf("get messages channel: %w", err)
	}

	return ch, nil
}

func (l *WebSocketListener) Close(_ context.Context) error {
	l.handler = nil
	return nil
}
