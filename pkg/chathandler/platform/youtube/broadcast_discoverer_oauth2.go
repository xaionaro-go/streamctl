package youtube

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	youtubesvc "google.golang.org/api/youtube/v3"
)

// DiscoverBroadcastViaOAuth2 polls for an active broadcast using the YouTube
// Data API v3 directly (liveBroadcasts.list mine=true). This avoids requiring
// youtubeapiproxy. Blocks until a broadcast is found or ctx is cancelled.
func DiscoverBroadcastViaOAuth2(
	ctx context.Context,
	svc *youtubesvc.Service,
) (_ BroadcastResult, _err error) {
	logger.Tracef(ctx, "DiscoverBroadcastViaOAuth2")
	defer func() { logger.Tracef(ctx, "/DiscoverBroadcastViaOAuth2: %v", _err) }()

	logger.Debugf(ctx, "polling for active broadcast via OAuth2 (liveBroadcasts.list mine=true)")

	for {
		if ctx.Err() != nil {
			return BroadcastResult{}, ctx.Err()
		}

		result, err := detectBroadcastViaOAuth2(ctx, svc)
		if err == nil {
			logger.Debugf(ctx, "discovered broadcast via OAuth2: liveChatID=%s videoID=%s", result.LiveChatID, result.VideoID)
			return result, nil
		}

		logger.Debugf(ctx, "no active broadcast found via OAuth2: %v", err)
		if !sleepCtx(ctx, broadcastPollInterval) {
			return BroadcastResult{}, ctx.Err()
		}
	}
}

func detectBroadcastViaOAuth2(
	ctx context.Context,
	svc *youtubesvc.Service,
) (_ BroadcastResult, _err error) {
	logger.Tracef(ctx, "detectBroadcastViaOAuth2")
	defer func() { logger.Tracef(ctx, "/detectBroadcastViaOAuth2: %v", _err) }()

	resp, err := svc.LiveBroadcasts.
		List([]string{"id", "snippet"}).
		BroadcastStatus("active").
		Context(ctx).
		Do()
	if err != nil {
		return BroadcastResult{}, fmt.Errorf("liveBroadcasts.list: %w", err)
	}

	if len(resp.Items) == 0 {
		return BroadcastResult{}, fmt.Errorf("no active broadcasts found")
	}

	for _, b := range resp.Items {
		if b.Snippet != nil && b.Snippet.LiveChatId != "" {
			logger.Debugf(ctx, "found broadcast via OAuth2: video=%s title=%q chatId=%s",
				b.Id, b.Snippet.Title, b.Snippet.LiveChatId)
			return BroadcastResult{
				LiveChatID: b.Snippet.LiveChatId,
				VideoID:    b.Id,
			}, nil
		}
	}

	// Broadcasts exist but none had a liveChatId in snippet — try resolving
	// via videos.list for the first one.
	videoID := resp.Items[0].Id
	liveChatID, err := resolveVideoChatIDViaOAuth2(ctx, svc, videoID)
	if err != nil {
		return BroadcastResult{}, err
	}

	return BroadcastResult{LiveChatID: liveChatID, VideoID: videoID}, nil
}

func resolveVideoChatIDViaOAuth2(
	ctx context.Context,
	svc *youtubesvc.Service,
	videoID string,
) (_ string, _err error) {
	logger.Tracef(ctx, "resolveVideoChatIDViaOAuth2")
	defer func() { logger.Tracef(ctx, "/resolveVideoChatIDViaOAuth2: %v", _err) }()

	resp, err := svc.Videos.
		List([]string{"liveStreamingDetails"}).
		Id(videoID).
		Context(ctx).
		Do()
	if err != nil {
		return "", fmt.Errorf("videos.list for %s: %w", videoID, err)
	}

	if len(resp.Items) == 0 {
		return "", fmt.Errorf("video %s not found", videoID)
	}

	details := resp.Items[0].LiveStreamingDetails
	if details == nil || details.ActiveLiveChatId == "" {
		return "", fmt.Errorf("video %s has no active live chat", videoID)
	}

	return details.ActiveLiveChatId, nil
}
