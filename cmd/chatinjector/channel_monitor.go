package main

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
	channelPollInterval   = 30 * time.Second
	maxPageBodySize       = 2 * 1024 * 1024
	httpUserAgent         = "Mozilla/5.0"
	httpAcceptLanguage    = "en-US,en;q=0.9"
	broadcastStatusActive = "active"
)

// DetectMethod controls how the channel monitor detects live streams.
type DetectMethod string

const (
	// DetectMethodHTML scrapes the channel's /live page for a videoId,
	// then verifies via ResolveLiveChatId. Works for public streams only.
	DetectMethodHTML = DetectMethod("html")

	// DetectMethodBroadcasts calls ListActiveBroadcasts (liveBroadcasts.list
	// with mine=true). Sees private/unlisted streams but only works for the
	// authenticated user's own channel.
	DetectMethodBroadcasts = DetectMethod("broadcasts")

	// DetectMethodSearch calls SearchLiveVideos (search.list with
	// channelId + eventType=live). Works for any channel but uses more quota.
	DetectMethodSearch = DetectMethod("search")
)

var videoIDPattern = regexp.MustCompile(`"videoId":"([a-zA-Z0-9_-]{11})"`)

// monitorChannel polls for active streams on a channel using the configured
// detection method. When a live stream is detected, it calls onLive with
// the liveChatId. When onLive returns (stream ended), it resumes polling.
func monitorChannel(
	ctx context.Context,
	ytConn grpc.ClientConnInterface,
	channelTarget string,
	detectMethod DetectMethod,
	onLive func(ctx context.Context, liveChatID string) error,
) (_err error) {
	logger.Tracef(ctx, "monitorChannel")
	defer func() { logger.Tracef(ctx, "/monitorChannel: %v", _err) }()

	adminClient := ytgrpc.NewAdminServiceClient(ytConn)
	logger.Infof(ctx, "monitoring channel %q for live streams (method: %s)", channelTarget, detectMethod)

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		liveChatID, err := detectLiveStream(ctx, adminClient, channelTarget, detectMethod)
		if err != nil {
			logger.Debugf(ctx, "no live stream detected: %v", err)
			if !sleep(ctx, channelPollInterval) {
				return ctx.Err()
			}
			continue
		}

		logger.Infof(ctx, "detected live stream: liveChatId=%s", liveChatID)

		if err := onLive(ctx, liveChatID); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			logger.Warnf(ctx, "bridge ended: %v", err)
		}

		logger.Debugf(ctx, "stream ended, resuming channel monitoring")
	}
}

func detectLiveStream(
	ctx context.Context,
	adminClient ytgrpc.AdminServiceClient,
	channelTarget string,
	detectMethod DetectMethod,
) (_ret string, _err error) {
	logger.Tracef(ctx, "detectLiveStream")
	defer func() { logger.Tracef(ctx, "/detectLiveStream: %v", _err) }()

	switch detectMethod {
	case DetectMethodBroadcasts:
		return detectViaBroadcasts(ctx, adminClient)
	case DetectMethodSearch:
		return detectViaSearch(ctx, adminClient, channelTarget)
	case DetectMethodHTML:
		return detectViaHTML(ctx, adminClient, channelTarget)
	default:
		return "", fmt.Errorf("unknown detect method: %q", detectMethod)
	}
}

func detectViaBroadcasts(
	ctx context.Context,
	adminClient ytgrpc.AdminServiceClient,
) (_ret string, _err error) {
	logger.Tracef(ctx, "detectViaBroadcasts")
	defer func() { logger.Tracef(ctx, "/detectViaBroadcasts: %v", _err) }()

	status := broadcastStatusActive
	resp, err := adminClient.ListActiveBroadcasts(ctx, &ytgrpc.ListActiveBroadcastsRequest{
		BroadcastStatus: &status,
	})
	if err != nil {
		return "", fmt.Errorf("ListActiveBroadcasts: %w", err)
	}

	if len(resp.Broadcasts) == 0 {
		return "", fmt.Errorf("no active broadcasts found")
	}

	for _, b := range resp.Broadcasts {
		if b.LiveChatId != "" {
			logger.Debugf(ctx, "found broadcast: video=%s title=%q chatId=%s", b.VideoId, b.Title, b.LiveChatId)
			return b.LiveChatId, nil
		}
	}

	// Broadcasts exist but none have a liveChatId — try resolving the first one.
	videoID := resp.Broadcasts[0].VideoId
	return resolveVideoToChat(ctx, adminClient, videoID)
}

func detectViaSearch(
	ctx context.Context,
	adminClient ytgrpc.AdminServiceClient,
	channelTarget string,
) (_ret string, _err error) {
	logger.Tracef(ctx, "detectViaSearch")
	defer func() { logger.Tracef(ctx, "/detectViaSearch: %v", _err) }()

	channelID := resolveChannelID(channelTarget)

	resp, err := adminClient.SearchLiveVideos(ctx, &ytgrpc.SearchLiveVideosRequest{
		ChannelId: channelID,
	})
	if err != nil {
		return "", fmt.Errorf("SearchLiveVideos: %w", err)
	}

	if len(resp.Videos) == 0 {
		return "", fmt.Errorf("no live videos found for channel %s", channelID)
	}

	videoID := resp.Videos[0].VideoId
	logger.Debugf(ctx, "found live video via search: video=%s title=%q", videoID, resp.Videos[0].Title)
	return resolveVideoToChat(ctx, adminClient, videoID)
}

func detectViaHTML(
	ctx context.Context,
	adminClient ytgrpc.AdminServiceClient,
	channelTarget string,
) (_ret string, _err error) {
	logger.Tracef(ctx, "detectViaHTML")
	defer func() { logger.Tracef(ctx, "/detectViaHTML: %v", _err) }()

	liveURL := channelLiveURL(channelTarget)
	videoID, err := extractVideoIDFromPage(ctx, liveURL)
	if err != nil {
		return "", err
	}

	return resolveVideoToChat(ctx, adminClient, videoID)
}

func resolveVideoToChat(
	ctx context.Context,
	adminClient ytgrpc.AdminServiceClient,
	videoID string,
) (_ret string, _err error) {
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
// For @handles, it returns the handle as-is (YouTube search API accepts handles).
// For UC... IDs, returns directly. For URLs, extracts the relevant part.
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
) (_ret string, _err error) {
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
