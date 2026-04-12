package youtube

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	ytpkg "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"google.golang.org/api/option"
	youtubesvc "google.golang.org/api/youtube/v3"
)

// PollingListener implements chathandler.ChatListener using YouTube Data API v3 polling.
type PollingListener struct {
	APIKey     string
	LiveChatID string
	VideoID    string
	listener   *ytpkg.ChatListener
}

func (l *PollingListener) Name() string { return "YouTube-Polling" }

func (l *PollingListener) Listen(
	ctx context.Context,
) (_ <-chan streamcontrol.Event, _err error) {
	logger.Tracef(ctx, "PollingListener.Listen")
	defer func() { logger.Tracef(ctx, "/PollingListener.Listen: %v", _err) }()

	svc, err := youtubesvc.NewService(ctx, option.WithAPIKey(l.APIKey))
	if err != nil {
		return nil, fmt.Errorf("create YouTube service: %w", err)
	}

	client := &APIKeyChatClient{Service: svc}

	listener, err := ytpkg.NewChatListener(ctx, client, l.VideoID, l.LiveChatID)
	if err != nil {
		return nil, fmt.Errorf("create chat listener: %w", err)
	}
	l.listener = listener

	return listener.MessagesChan(), nil
}

func (l *PollingListener) Close(ctx context.Context) error {
	if l.listener != nil {
		return l.listener.Close(ctx)
	}
	return nil
}
