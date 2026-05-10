package chatmessagesstorage

import (
	"context"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/xsync"
)

// UpsertMessage replaces an existing entry with the same (ID, Platform) or
// appends a new one when no match exists. Used by the translation worker:
// after the raw message is added, the worker re-publishes the translated
// variant via UpsertMessage so the archive holds the final form. Two-key
// match (not ID alone) because the same bare ID can appear across platforms.
func (s *ChatMessagesStorage) UpsertMessage(
	ctx context.Context,
	msg api.ChatMessage,
) error {
	return xsync.DoA2R1(ctx, &s.Mutex, s.upsertMessageLocked, ctx, msg)
}

func (s *ChatMessagesStorage) upsertMessageLocked(
	ctx context.Context,
	msg api.ChatMessage,
) (_err error) {
	logger.Tracef(ctx, "upsertMessageLocked(ctx, id=%q plat=%q)", msg.ID, msg.Platform)
	defer func() { logger.Tracef(ctx, "/upsertMessageLocked(ctx, id=%q plat=%q): %v", msg.ID, msg.Platform, _err) }()

	for idx := range s.Messages {
		if s.Messages[idx].ID != msg.ID || s.Messages[idx].Platform != msg.Platform {
			continue
		}
		// Mirror addMessageLocked's archive invariant: stored messages are
		// not "live" — IsLive is the live-feed flag, not a storage property.
		msg.IsLive = false
		s.Messages[idx] = msg
		s.IsChanged = true
		return nil
	}
	s.addMessageLocked(ctx, msg)
	return nil
}
