package chatmessagesstorage

import (
	"context"
	"sort"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
)

func msgLess(ctx context.Context, a *api.ChatMessage, b *api.ChatMessage) bool {
	if a.CreatedAt != b.CreatedAt {
		return a.CreatedAt.Before(b.CreatedAt)
	}
	if a.Platform != b.Platform {
		return a.Platform < b.Platform
	}
	if a.Username != b.Username {
		return a.Username < b.Username
	}
	if a.MessageID != b.MessageID {
		return a.MessageID < b.MessageID
	}
	if a.Message != b.Message {
		return a.Message < b.Message
	}
	if a != b {
		logger.Errorf(ctx, "msgs A and B look equal, but are not: A:%#+v B:%#+v", a, b)
	}
	return false
}

func (s *ChatMessagesStorage) sortAndDeduplicateAndTruncate(ctx context.Context) {
	if len(s.Messages) == 0 {
		return
	}

	sort.Slice(s.Messages, func(i, j int) bool {
		return msgLess(ctx, &s.Messages[i], &s.Messages[j])
	})
	s.IsSorted = true

	dedup := make([]api.ChatMessage, 0, len(s.Messages))
	dedup = append(dedup, s.Messages[0])
	for _, msg := range s.Messages[1:] {
		if msg == dedup[len(dedup)-1] {
			continue
		}
		dedup = append(dedup, msg)
	}

	s.Messages = dedup
	if len(s.Messages) > MaxMessages {
		s.Messages = s.Messages[len(s.Messages)-MaxMessages:]
	}
}
