package youtube

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	ytpkg "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
)

// ObsoleteJSONListener implements chathandler.ChatListener using the obsolete
// YouTube JSON parser (ChatListenerOBSOLETE). This does not require API keys
// but scrapes the YouTube live chat page directly.
type ObsoleteJSONListener struct {
	VideoID  string
	listener *ytpkg.ChatListenerOBSOLETE
}

func (l *ObsoleteJSONListener) Name() string { return "YouTube-JSON-Obsolete" }

func (l *ObsoleteJSONListener) Listen(
	ctx context.Context,
) (_ <-chan streamcontrol.Event, _err error) {
	logger.Tracef(ctx, "ObsoleteJSONListener.Listen")
	defer func() { logger.Tracef(ctx, "/ObsoleteJSONListener.Listen: %v", _err) }()

	listener, err := ytpkg.NewChatListenerOBSOLETE(ctx, l.VideoID, nil)
	if err != nil {
		return nil, fmt.Errorf("create obsolete YouTube listener: %w", err)
	}
	l.listener = listener

	return listener.MessagesChan(), nil
}

func (l *ObsoleteJSONListener) Close(ctx context.Context) error {
	if l.listener != nil {
		return l.listener.Close(ctx)
	}
	return nil
}
