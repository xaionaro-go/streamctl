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
	channelPollInterval = 30 * time.Second
	maxPageBodySize     = 2 * 1024 * 1024
)

var videoIDPattern = regexp.MustCompile(`"videoId":"([a-zA-Z0-9_-]{11})"`)

// monitorChannel polls a YouTube channel's /live page for active streams.
// When a live stream is detected, it calls onLive with the liveChatId.
// When onLive returns (stream ended), it resumes polling.
func monitorChannel(
	ctx context.Context,
	ytConn grpc.ClientConnInterface,
	channelTarget string,
	onLive func(ctx context.Context, liveChatID string) error,
) (_err error) {
	logger.Tracef(ctx, "monitorChannel")
	defer func() { logger.Tracef(ctx, "/monitorChannel: %v", _err) }()

	liveURL := channelLiveURL(channelTarget)
	logger.Infof(ctx, "monitoring %s for live streams", liveURL)

	adminClient := ytgrpc.NewAdminServiceClient(ytConn)

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		videoID, err := extractVideoIDFromPage(ctx, liveURL)
		if err != nil {
			logger.Debugf(ctx, "failed to extract video ID from %s: %v", liveURL, err)
			if !sleep(ctx, channelPollInterval) {
				return ctx.Err()
			}
			continue
		}

		liveChatID, err := adminClient.ResolveLiveChatId(ctx, &ytgrpc.ResolveLiveChatIdRequest{
			VideoId: videoID,
		})
		if err != nil {
			logger.Debugf(ctx, "video %s is not live (ResolveLiveChatId: %v)", videoID, err)
			if !sleep(ctx, channelPollInterval) {
				return ctx.Err()
			}
			continue
		}

		logger.Infof(ctx, "detected live stream: video=%s liveChatId=%s", videoID, liveChatID.LiveChatId)

		if err := onLive(ctx, liveChatID.LiveChatId); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			logger.Warnf(ctx, "bridge ended for video %s: %v", videoID, err)
		}

		logger.Debugf(ctx, "stream ended, resuming channel monitoring")
	}
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
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

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
