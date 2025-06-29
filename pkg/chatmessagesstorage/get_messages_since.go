package chatmessagesstorage

import (
	"context"
	"slices"
	"sort"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/xsync"
)

func (s *ChatMessagesStorage) GetMessagesSince(
	ctx context.Context,
	since time.Time,
	limit uint,
) ([]api.ChatMessage, error) {
	return xsync.DoA3R2(ctx, &s.Mutex, s.getMessagesSinceLocked, ctx, since, limit)
}

func (s *ChatMessagesStorage) getMessagesSinceLocked(
	ctx context.Context,
	since time.Time,
	limit uint,
) (_ret []api.ChatMessage, _err error) {
	logger.Tracef(ctx, "getMessagesSinceLocked(ctx, %v, %d)", since, limit)
	defer func() {
		logger.Tracef(ctx, "/getMessagesSinceLocked(ctx, %v, %d): len:%d, %v", since, limit, len(_ret), _err)
	}()

	if len(s.Messages) == 0 {
		logger.Tracef(ctx, "len(s.Messages) == 0")
		return nil, nil
	}

	if !s.IsSorted {
		logger.Tracef(ctx, "not sorted, sorting")
		s.sortAndDeduplicateAndTruncate(ctx)
	}

	idx := sort.Search(len(s.Messages), func(i int) bool {
		m := &s.Messages[i]
		return !m.CreatedAt.Before(since)
	})
	logger.Tracef(ctx, "search result index: %d", idx)

	if idx >= len(s.Messages) {
		lastMessage := s.Messages[len(s.Messages)-1]
		if !since.Before(lastMessage.CreatedAt) {
			logger.Tracef(ctx, "all messages are too old: %v < %v", lastMessage, since)
			return nil, nil
		}
		idx = 0
	}

	if limit > 0 && len(s.Messages)-idx > int(limit) {
		oldIdx := idx
		idx = len(s.Messages) - int(limit)
		logger.Tracef(ctx, "corrected the idx from %d to %d as per the count limit", oldIdx, idx)
	}

	if idx < len(s.Messages) {
		if s.Messages[idx].CreatedAt.Before(since) {
			logger.Errorf(ctx, "internal error, for some reason we loaded messages older than %v, for example %v", since, s.Messages[idx])
		}
	}

	logger.Tracef(ctx, "s.Messages[%d:%d]", idx, len(s.Messages))
	return slices.Clone(s.Messages[idx:]), nil
}
