package chatmessagesstorage

import (
	"context"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/xsync"
)

func (s *ChatMessagesStorage) AddMessage(
	ctx context.Context,
	msg api.ChatMessage,
) error {
	xsync.DoA2(ctx, &s.Mutex, s.addMessageLocked, ctx, msg)
	return nil
}

func (s *ChatMessagesStorage) addMessageLocked(
	ctx context.Context,
	msg api.ChatMessage,
) {
	logger.Tracef(ctx, "addMessageLocked(ctx, %#+v)", msg)
	defer func() { logger.Tracef(ctx, "/addMessageLocked(ctx, %#+v)", msg) }()

	if len(s.Messages) > 0 && !msg.CreatedAt.After(s.Messages[len(s.Messages)-1].CreatedAt) {
		s.IsSorted = false
	}
	s.Messages = append(s.Messages, msg)
	s.IsChanged = true
	if len(s.Messages) <= MaxMessages {
		return
	}
	if s.IsSorted {
		s.Messages = s.Messages[len(s.Messages)-MaxMessages:]
		return
	}
	s.sortAndDeduplicateAndTruncate(ctx)
}
