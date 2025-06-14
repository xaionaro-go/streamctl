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
) ([]api.ChatMessage, error) {
	return xsync.DoA2R2(ctx, &s.Mutex, s.getMessagesSinceLocked, ctx, since)
}

func (s *ChatMessagesStorage) getMessagesSinceLocked(
	ctx context.Context,
	since time.Time,
) (_ret []api.ChatMessage, _err error) {
	logger.Tracef(ctx, "getMessagesSinceLocked(ctx, %v)", since)
	defer func() { logger.Tracef(ctx, "/getMessagesSinceLocked(ctx, %v): len:%d, %v", since, len(_ret), _err) }()

	if len(s.Messages) == 0 {
		return nil, nil
	}

	if !s.IsSorted {
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

	return slices.Clone(s.Messages[idx:]), nil
}
