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
		return nil, nil
	}

	if !s.IsSorted {
		logger.Tracef(ctx, "not sorted, sorting")
		s.sortAndDeduplicateAndTruncate(ctx)
	}

	idx := sort.Search(len(s.Messages), func(i int) bool {
		m := &s.Messages[i]
		return !m.CreatedAt.After(since)
	})

	if idx >= len(s.Messages) {
		if !since.Before(s.Messages[0].CreatedAt) {
			return nil, nil
		}
		idx = 0
	}

	if limit > 0 && len(s.Messages)-idx > int(limit) {
		idx = len(s.Messages) - int(limit)
	}

	return slices.Clone(s.Messages[idx:]), nil
}
