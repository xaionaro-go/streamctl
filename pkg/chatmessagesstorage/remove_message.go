package chatmessagesstorage

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/xsync"
)

func (s *ChatMessagesStorage) RemoveMessage(
	ctx context.Context,
	msgID streamcontrol.ChatMessageID,
) error {
	return xsync.DoA2R1(ctx, &s.Mutex, s.removeMessageLocked, ctx, msgID)
}

func (s *ChatMessagesStorage) removeMessageLocked(
	ctx context.Context,
	msgID streamcontrol.ChatMessageID,
) (_err error) {
	logger.Tracef(ctx, "removeMessageLocked(ctx, '%v')", msgID)
	defer func() { logger.Tracef(ctx, "/removeMessageLocked(ctx, '%v'): %v", msgID, _err) }()

	for idx := range s.Messages {
		if s.Messages[idx].MessageID != msgID {
			continue
		}
		s.Messages[idx] = s.Messages[len(s.Messages)-1]
		s.Messages = s.Messages[:len(s.Messages)-1]
		s.IsSorted = false
		s.IsChanged = true
		return nil
	}
	return fmt.Errorf("message with ID '%s' was not found", msgID)
}
