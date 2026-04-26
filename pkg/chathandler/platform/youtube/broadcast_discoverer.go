package youtube

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/youtubeapiproxy/grpc/ytgrpc"
	"google.golang.org/grpc"
)

const (
	broadcastPollInterval = 30 * time.Second
	maxPageBodySize       = 2 * 1024 * 1024
	httpUserAgent         = "Mozilla/5.0"
	httpAcceptLanguage    = "en-US,en;q=0.9"
	broadcastStatusActive = "active"
)

// DetectMethod controls how broadcast discovery detects live streams.
type DetectMethod string

const (
	// DetectMethodBroadcasts calls ListActiveBroadcasts (liveBroadcasts.list
	// with mine=true). Sees private/unlisted streams but only works for the
	// authenticated user's own channel.
	DetectMethodBroadcasts = DetectMethod("broadcasts")

	// DetectMethodSearch calls SearchLiveVideos (search.list with
	// channelId + eventType=live). Works for any channel but uses more quota.
	DetectMethodSearch = DetectMethod("search")

	// DetectMethodHTML scrapes the channel's /live page for a videoId,
	// then verifies via ResolveLiveChatId. Works for public streams only.
	DetectMethodHTML = DetectMethod("html")
)

var videoIDPattern = regexp.MustCompile(`"videoId":"([a-zA-Z0-9_-]{11})"`)

// BroadcastResult holds the outcome of a successful broadcast discovery.
type BroadcastResult struct {
	LiveChatID string
	VideoID    string
}

// DiscoverBroadcast polls for an active broadcast on the channel using the
// configured detection method. Returns as soon as one is found. Blocks until
// a broadcast is detected or ctx is cancelled. When needLiveChatID is false
// the discoverer skips the ResolveLiveChatId proxy RPC; the OBSOLETE chat
// scraper only needs the videoID, so paying that round-trip is wasted.
func DiscoverBroadcast(
	ctx context.Context,
	conn grpc.ClientConnInterface,
	channelID string,
	detectMethod DetectMethod,
	needLiveChatID bool,
) (_ BroadcastResult, _err error) {
	logger.Tracef(ctx, "DiscoverBroadcast")
	defer func() { logger.Tracef(ctx, "/DiscoverBroadcast: %v", _err) }()

	adminClient := ytgrpc.NewAdminServiceClient(conn)
	logger.Debugf(ctx, "polling for active broadcast (method: %s, channel: %q, needLiveChatID: %t)",
		detectMethod, channelID, needLiveChatID)

	for {
		if ctx.Err() != nil {
			return BroadcastResult{}, ctx.Err()
		}

		result, err := detectLiveStream(ctx, adminClient, channelID, detectMethod, needLiveChatID)
		if err == nil {
			logger.Debugf(ctx, "discovered broadcast: liveChatID=%s videoID=%s", result.LiveChatID, result.VideoID)
			return result, nil
		}

		logger.Debugf(ctx, "no live stream detected: %v", err)
		if !sleepCtx(ctx, broadcastPollInterval) {
			return BroadcastResult{}, ctx.Err()
		}
	}
}

func detectLiveStream(
	ctx context.Context,
	adminClient ytgrpc.AdminServiceClient,
	channelID string,
	detectMethod DetectMethod,
	needLiveChatID bool,
) (_ BroadcastResult, _err error) {
	logger.Tracef(ctx, "detectLiveStream")
	defer func() { logger.Tracef(ctx, "/detectLiveStream: %v", _err) }()

	switch detectMethod {
	case DetectMethodBroadcasts:
		return detectViaBroadcasts(ctx, adminClient, needLiveChatID)
	case DetectMethodSearch:
		return detectViaSearch(ctx, adminClient, channelID, needLiveChatID)
	case DetectMethodHTML:
		return detectViaHTML(ctx, adminClient, channelID, needLiveChatID)
	default:
		return BroadcastResult{}, fmt.Errorf("unknown detect method: %q", detectMethod)
	}
}

func detectViaBroadcasts(
	ctx context.Context,
	adminClient ytgrpc.AdminServiceClient,
	needLiveChatID bool,
) (_ BroadcastResult, _err error) {
	logger.Tracef(ctx, "detectViaBroadcasts")
	defer func() { logger.Tracef(ctx, "/detectViaBroadcasts: %v", _err) }()

	status := broadcastStatusActive
	resp, err := adminClient.ListActiveBroadcasts(ctx, &ytgrpc.ListActiveBroadcastsRequest{
		BroadcastStatus: &status,
	})
	if err != nil {
		return BroadcastResult{}, fmt.Errorf("ListActiveBroadcasts: %w", err)
	}

	if len(resp.Broadcasts) == 0 {
		return BroadcastResult{}, fmt.Errorf("no active broadcasts found")
	}

	for _, b := range resp.Broadcasts {
		if b.LiveChatId != "" {
			logger.Debugf(ctx, "found broadcast: video=%s title=%q chatId=%s", b.VideoId, b.Title, b.LiveChatId)
			return BroadcastResult{
				LiveChatID: b.LiveChatId,
				VideoID:    b.VideoId,
			}, nil
		}
	}

	videoID := resp.Broadcasts[0].VideoId
	if !needLiveChatID {
		return BroadcastResult{VideoID: videoID}, nil
	}

	// Broadcasts exist but none have a liveChatId -- resolve via the first one.
	liveChatID, err := resolveVideoToChat(ctx, adminClient, videoID)
	if err != nil {
		return BroadcastResult{}, err
	}
	return BroadcastResult{LiveChatID: liveChatID, VideoID: videoID}, nil
}

func detectViaSearch(
	ctx context.Context,
	adminClient ytgrpc.AdminServiceClient,
	channelID string,
	needLiveChatID bool,
) (_ BroadcastResult, _err error) {
	logger.Tracef(ctx, "detectViaSearch")
	defer func() { logger.Tracef(ctx, "/detectViaSearch: %v", _err) }()

	resolvedID := resolveChannelID(channelID)

	resp, err := adminClient.SearchLiveVideos(ctx, &ytgrpc.SearchLiveVideosRequest{
		ChannelId: resolvedID,
	})
	if err != nil {
		return BroadcastResult{}, fmt.Errorf("SearchLiveVideos: %w", err)
	}

	if len(resp.Videos) == 0 {
		return BroadcastResult{}, fmt.Errorf("no live videos found for channel %s", resolvedID)
	}

	videoID := resp.Videos[0].VideoId
	logger.Debugf(ctx, "found live video via search: video=%s title=%q", videoID, resp.Videos[0].Title)

	if !needLiveChatID {
		return BroadcastResult{VideoID: videoID}, nil
	}

	liveChatID, err := resolveVideoToChat(ctx, adminClient, videoID)
	if err != nil {
		return BroadcastResult{}, err
	}
	return BroadcastResult{LiveChatID: liveChatID, VideoID: videoID}, nil
}

func detectViaHTML(
	ctx context.Context,
	adminClient ytgrpc.AdminServiceClient,
	channelID string,
	needLiveChatID bool,
) (_ BroadcastResult, _err error) {
	logger.Tracef(ctx, "detectViaHTML")
	defer func() { logger.Tracef(ctx, "/detectViaHTML: %v", _err) }()

	liveURL := channelLiveURL(channelID)
	videoID, err := extractVideoIDFromPage(ctx, liveURL)
	if err != nil {
		return BroadcastResult{}, err
	}

	if !needLiveChatID {
		return BroadcastResult{VideoID: videoID}, nil
	}

	liveChatID, err := resolveVideoToChat(ctx, adminClient, videoID)
	if err != nil {
		return BroadcastResult{}, err
	}
	return BroadcastResult{LiveChatID: liveChatID, VideoID: videoID}, nil
}

func resolveVideoToChat(
	ctx context.Context,
	adminClient ytgrpc.AdminServiceClient,
	videoID string,
) (_ string, _err error) {
	logger.Tracef(ctx, "resolveVideoToChat")
	defer func() { logger.Tracef(ctx, "/resolveVideoToChat: %v", _err) }()

	resp, err := adminClient.ResolveLiveChatId(ctx, &ytgrpc.ResolveLiveChatIdRequest{
		VideoId: videoID,
	})
	if err != nil {
		return "", fmt.Errorf("video %s: %w", videoID, err)
	}

	return resp.LiveChatId, nil
}

// resolveChannelID extracts a channel ID from the target.
// For @handles, returns as-is (YouTube search API accepts handles).
// For UC... IDs, returns directly. For URLs, extracts relevant part.
func resolveChannelID(target string) string {
	switch {
	case strings.HasPrefix(target, "UC"):
		return target
	case strings.HasPrefix(target, "@"):
		return target
	case strings.Contains(target, "/channel/"):
		parts := strings.Split(target, "/channel/")
		if len(parts) >= 2 {
			return strings.TrimRight(parts[1], "/")
		}
	case strings.Contains(target, "/@"):
		parts := strings.Split(target, "/@")
		if len(parts) >= 2 {
			return "@" + strings.TrimRight(parts[1], "/")
		}
	}
	return target
}

func channelLiveURL(target string) string {
	switch {
	case strings.HasPrefix(target, "https://"),
		strings.HasPrefix(target, "http://"):
		return strings.TrimRight(target, "/") + "/live"
	case strings.HasPrefix(target, "@"):
		return "https://www.youtube.com/" + target + "/live"
	case strings.HasPrefix(target, "UC"):
		return "https://www.youtube.com/channel/" + target + "/live"
	default:
		return "https://www.youtube.com/" + target + "/live"
	}
}

func extractVideoIDFromPage(
	ctx context.Context,
	url string,
) (_ string, _err error) {
	logger.Tracef(ctx, "extractVideoIDFromPage")
	defer func() { logger.Tracef(ctx, "/extractVideoIDFromPage: %v", _err) }()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", httpUserAgent)
	req.Header.Set("Accept-Language", httpAcceptLanguage)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBodySize))
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	matches := videoIDPattern.FindSubmatch(body)
	if matches == nil {
		return "", fmt.Errorf("no videoId found in page")
	}

	return string(matches[1]), nil
}

// sleepCtx waits for duration d or until ctx is cancelled.
// Returns true if the sleep completed, false if cancelled.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
