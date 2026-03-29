package chatmessagesstorage

import (
	"context"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/xsync"
)

func (s *ChatMessagesStorage) PurgeMessages(
	ctx context.Context,
	platID *streamcontrol.PlatformID,
	streamID *streamcontrol.StreamID,
) (uint64, error) {
	return xsync.DoA3R2(ctx, &s.Mutex, s.purgeMessagesLocked, ctx, platID, streamID)
}

func (s *ChatMessagesStorage) purgeMessagesLocked(
	ctx context.Context,
	platID *streamcontrol.PlatformID,
	streamID *streamcontrol.StreamID,
) (_count uint64, _err error) {
	logger.Tracef(ctx, "purgeMessagesLocked(ctx, %v, %v)", platID, streamID)
	defer func() { logger.Tracef(ctx, "/purgeMessagesLocked(ctx, %v, %v): count=%d %v", platID, streamID, _count, _err) }()

	kept := s.Messages[:0]
	for _, msg := range s.Messages {
		if platID != nil && msg.Platform != *platID {
			kept = append(kept, msg)
			continue
		}
		if streamID != nil && msg.StreamID != *streamID {
			kept = append(kept, msg)
			continue
		}
		_count++
	}

	if _count == 0 {
		return 0, nil
	}

	s.Messages = kept
	s.IsChanged = true
	return _count, nil
}
