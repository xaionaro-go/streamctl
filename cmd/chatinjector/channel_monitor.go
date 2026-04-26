package main

import (
	"context"

	"github.com/facebookincubator/go-belt/tool/logger"
	ytlistener "github.com/xaionaro-go/streamctl/pkg/chathandler/platform/youtube"
	"google.golang.org/grpc"
)

// DetectMethod is a local alias for the shared youtube.DetectMethod type.
type DetectMethod = ytlistener.DetectMethod

const (
	DetectMethodHTML       = ytlistener.DetectMethodHTML
	DetectMethodBroadcasts = ytlistener.DetectMethodBroadcasts
	DetectMethodSearch     = ytlistener.DetectMethodSearch
)

// monitorChannel polls for active streams on a channel using the configured
// detection method. When a live stream is detected, it calls onLive with
// the liveChatId. When onLive returns (stream ended), it resumes polling.
//
// Delegates broadcast discovery to the shared youtube package.
func monitorChannel(
	ctx context.Context,
	ytConn grpc.ClientConnInterface,
	channelTarget string,
	detectMethod DetectMethod,
	onLive func(ctx context.Context, liveChatID string) error,
) (_err error) {
	logger.Tracef(ctx, "monitorChannel")
	defer func() { logger.Tracef(ctx, "/monitorChannel: %v", _err) }()

	logger.Debugf(ctx, "monitoring channel %q for live streams (method: %s)", channelTarget, detectMethod)

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		result, err := ytlistener.DiscoverBroadcast(ctx, ytConn, channelTarget, detectMethod, true)
		if err != nil {
			// DiscoverBroadcast only returns error on ctx cancellation.
			return err
		}

		logger.Debugf(ctx, "detected live stream: liveChatId=%s", result.LiveChatID)

		if err := onLive(ctx, result.LiveChatID); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			logger.Warnf(ctx, "bridge ended: %v", err)
		}

		logger.Debugf(ctx, "stream ended, resuming channel monitoring")
	}
}
