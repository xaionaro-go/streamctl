package kick

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
)

// ObsoleteListener implements chathandler.ChatListener using the obsolete Kick HTTP handler.
type ObsoleteListener struct {
	ChannelSlug string
	handler     *kick.ChatHandlerOBSOLETE
}

func (l *ObsoleteListener) Name() string { return "HTTP-Obsolete" }

func (l *ObsoleteListener) Listen(
	ctx context.Context,
) (_ <-chan streamcontrol.Event, _err error) {
	logger.Tracef(ctx, "ObsoleteListener.Listen")
	defer func() { logger.Tracef(ctx, "/ObsoleteListener.Listen: %v", _err) }()

	h, err := kick.NewChatHandlerOBSOLETE(ctx, l.ChannelSlug)
	if err != nil {
		return nil, fmt.Errorf("create obsolete Kick handler: %w", err)
	}
	l.handler = h

	ch, err := h.GetMessagesChan(ctx)
	if err != nil {
		return nil, fmt.Errorf("get messages channel: %w", err)
	}

	return ch, nil
}

func (l *ObsoleteListener) Close(_ context.Context) error {
	l.handler = nil
	return nil
}
