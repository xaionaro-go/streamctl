package streamd

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

func (d *StreamD) PurgeChatMessages(
	ctx context.Context,
	platID *streamcontrol.PlatformID,
	streamID *streamcontrol.StreamID,
) (_ uint64, _err error) {
	logger.Debugf(ctx, "PurgeChatMessages(ctx, %v, %v)", platID, streamID)
	defer func() { logger.Debugf(ctx, "/PurgeChatMessages(ctx, %v, %v): %v", platID, streamID, _err) }()

	count, err := d.ChatMessagesStorage.PurgeMessages(ctx, platID, streamID)
	if err != nil {
		return 0, fmt.Errorf("unable to purge chat messages: %w", err)
	}
	return count, nil
}
