package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	youtube "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube/types"
	"github.com/xaionaro-go/youtubeapiproxy/grpc/ytgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// YouTubeSource implements ChatSource by connecting to a youtubeapiproxy gRPC
// server and streaming live-chat messages for a video or channel.
type YouTubeSource struct {
	ProxyAddr    string
	Video        string
	Channel      string
	DetectMethod string
	Hl           string
	RawMessage   bool
}

func (s *YouTubeSource) PlatformID() streamcontrol.PlatformName {
	return youtube.ID
}

// Run connects to the youtubeapiproxy, resolves the live chat target, and
// streams events until ctx is cancelled. For channel mode it polls for new
// streams automatically.
func (s *YouTubeSource) Run(
	ctx context.Context,
	events chan<- ChatEvent,
) (_err error) {
	logger.Tracef(ctx, "YouTubeSource.Run")
	defer func() { logger.Tracef(ctx, "/YouTubeSource.Run: %v", _err) }()

	ytConn, err := grpc.NewClient(s.ProxyAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("connect to youtubeapiproxy at %s: %w", s.ProxyAddr, err)
	}
	defer ytConn.Close()

	chatClient := ytgrpc.NewV3DataLiveChatMessageServiceClient(ytConn)

	detectMethod := DetectMethod(s.DetectMethod)
	if detectMethod == "" {
		detectMethod = DetectMethodSearch
	}

	if s.Channel != "" {
		return monitorChannel(ctx, ytConn, s.Channel, detectMethod, func(ctx context.Context, liveChatID string) error {
			return s.bridgeChat(ctx, chatClient, liveChatID, events)
		})
	}

	liveChatID, err := resolveLiveChatID(ctx, ytConn, s.Video)
	if err != nil {
		return fmt.Errorf("resolve live chat ID for %q: %w", s.Video, err)
	}
	logger.Infof(ctx, "resolved live chat ID: %s", liveChatID)

	return s.bridgeChat(ctx, chatClient, liveChatID, events)
}

// bridgeChat streams messages for a single liveChatID, emitting ChatEvents.
// It handles reconnects on transient errors and returns nil when the live
// chat ends (so monitorChannel can detect the next stream).
func (s *YouTubeSource) bridgeChat(
	ctx context.Context,
	chatClient ytgrpc.V3DataLiveChatMessageServiceClient,
	liveChatID string,
	events chan<- ChatEvent,
) (_err error) {
	logger.Tracef(ctx, "YouTubeSource.bridgeChat")
	defer func() { logger.Tracef(ctx, "/YouTubeSource.bridgeChat: %v", _err) }()

	var pageToken string

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		err := s.recvAndEmit(ctx, chatClient, liveChatID, &pageToken, events)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		switch {
		case err == nil:
			// Stream ended cleanly (EOF) -- reconnect immediately to
			// minimize latency between message batches.
			logger.Debugf(ctx, "stream ended, reconnecting immediately")
			continue
		case isStreamEndedError(err):
			// The live chat no longer exists (stream ended).
			// Return so the caller (monitorChannel) can detect the next stream.
			logger.Infof(ctx, "live chat ended: %v", err)
			return nil
		default:
			logger.Warnf(ctx, "stream error, reconnecting in %s: %v", reconnectDelay, err)
			if !sleep(ctx, reconnectDelay) {
				return ctx.Err()
			}
		}
	}
}

// recvAndEmit opens one StreamList call, converts each message to a ChatEvent,
// and sends it on the events channel. Translation is NOT done here -- the
// engine handles it.
func (s *YouTubeSource) recvAndEmit(
	ctx context.Context,
	chatClient ytgrpc.V3DataLiveChatMessageServiceClient,
	liveChatID string,
	pageToken *string,
	events chan<- ChatEvent,
) (_err error) {
	logger.Tracef(ctx, "YouTubeSource.recvAndEmit")
	defer func() { logger.Tracef(ctx, "/YouTubeSource.recvAndEmit: %v", _err) }()

	stream, err := chatClient.StreamList(ctx, &ytgrpc.LiveChatMessageListRequest{
		LiveChatId: liveChatID,
		Hl:         s.Hl,
		Part:       []string{"snippet", "authorDetails"},
		PageToken:  *pageToken,
	})
	if err != nil {
		return fmt.Errorf("StreamList: %w", err)
	}

	for {
		resp, err := stream.Recv()
		switch {
		case errors.Is(err, io.EOF):
			return nil
		case err != nil:
			return fmt.Errorf("stream recv: %w", err)
		}

		if resp.NextPageToken != "" {
			*pageToken = resp.NextPageToken
		}

		for _, item := range resp.Items {
			if snippet := item.GetSnippet(); snippet != nil {
				logger.Debugf(ctx, "received yt event: id=%s type=%s user=%s msg=%q",
					item.GetId(), snippet.GetType(), item.GetAuthorDetails().GetDisplayName(),
					snippet.GetDisplayMessage())
			}

			ev := convertMessage(ctx, item, s.RawMessage)

			select {
			case events <- ChatEvent{Event: ev, Platform: youtube.ID}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

// resolveLiveChatID determines the liveChatId from the user-provided target.
// If the target looks like a video URL or video ID, it calls ResolveLiveChatId
// on the admin service. Otherwise it assumes the target is already a liveChatId.
func resolveLiveChatID(
	ctx context.Context,
	conn grpc.ClientConnInterface,
	target string,
) (_ret string, _err error) {
	logger.Tracef(ctx, "resolveLiveChatID")
	defer func() { logger.Tracef(ctx, "/resolveLiveChatID: %v", _err) }()

	switch {
	case strings.Contains(target, "youtube.com"),
		strings.Contains(target, "youtu.be"),
		strings.Contains(target, "://"):
		// URL -- resolve via admin RPC.
	case len(target) == 11:
		// Likely a video ID (YouTube video IDs are 11 chars).
	default:
		return target, nil
	}

	adminClient := ytgrpc.NewAdminServiceClient(conn)
	resp, err := adminClient.ResolveLiveChatId(ctx, &ytgrpc.ResolveLiveChatIdRequest{
		VideoId: target,
	})
	if err != nil {
		return "", fmt.Errorf("ResolveLiveChatId RPC: %w", err)
	}

	return resp.LiveChatId, nil
}
