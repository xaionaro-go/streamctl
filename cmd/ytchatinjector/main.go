package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	scgoconv "github.com/xaionaro-go/streamctl/pkg/streamcontrol/protobuf/goconv"
	youtube "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube/types"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/youtubeapiproxy/grpc/ytgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

const reconnectDelay = 5 * time.Second

func main() {
	ytProxyAddr := pflag.String("yt-proxy-addr", "", "youtubeapiproxy gRPC address (host:port)")
	streamdAddr := pflag.String("streamd-addr", "", "streamd gRPC address (host:port)")
	video := pflag.String("video", "", "video URL, video ID, or liveChatId")
	channel := pflag.String("channel", "", "channel URL, @handle, or channel ID to monitor for live streams")
	hl := pflag.String("hl", "", "language for YouTube system messages (e.g. en, de, ja)")
	var logLevel logger.Level
	pflag.Var(&logLevel, "log-level", "log level")
	pflag.Parse()

	if logLevel == logger.LevelUndefined {
		logLevel = logger.LevelWarning
	}
	observability.LogLevelFilter.SetLevel(logLevel)

	l := logrus.Default()
	ctx := logger.CtxWithLogger(context.Background(), l)
	ctx = logger.CtxWithLogger(ctx, l.WithLevel(logLevel))
	defer belt.Flush(ctx)

	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()

	if err := run(ctx, *ytProxyAddr, *streamdAddr, *video, *channel, *hl); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run(
	ctx context.Context,
	ytProxyAddr string,
	streamdAddr string,
	video string,
	channel string,
	hl string,
) (_err error) {
	logger.Tracef(ctx, "run")
	defer func() { logger.Tracef(ctx, "/run: %v", _err) }()

	switch {
	case ytProxyAddr == "":
		return fmt.Errorf("--yt-proxy-addr is required")
	case streamdAddr == "":
		return fmt.Errorf("--streamd-addr is required")
	case video == "" && channel == "":
		return fmt.Errorf("either --video or --channel is required")
	case video != "" && channel != "":
		return fmt.Errorf("--video and --channel are mutually exclusive")
	}

	ytConn, err := grpc.NewClient(ytProxyAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("connect to youtubeapiproxy at %s: %w", ytProxyAddr, err)
	}
	defer ytConn.Close()

	sdCreds := detectTransportCredentials(ctx, streamdAddr)
	sdConn, err := grpc.NewClient(streamdAddr,
		grpc.WithTransportCredentials(sdCreds),
	)
	if err != nil {
		return fmt.Errorf("connect to streamd at %s: %w", streamdAddr, err)
	}
	defer sdConn.Close()

	chatClient := ytgrpc.NewV3DataLiveChatMessageServiceClient(ytConn)
	streamdClient := streamd_grpc.NewStreamDClient(sdConn)

	if channel != "" {
		return monitorChannel(ctx, ytConn, channel, func(ctx context.Context, liveChatID string) error {
			return bridgeChat(ctx, chatClient, streamdClient, liveChatID, hl)
		})
	}

	liveChatID, err := resolveLiveChatID(ctx, ytConn, video)
	if err != nil {
		return fmt.Errorf("resolve live chat ID for %q: %w", video, err)
	}
	logger.Infof(ctx, "resolved live chat ID: %s", liveChatID)

	return bridgeChat(ctx, chatClient, streamdClient, liveChatID, hl)
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
		// URL — resolve via admin RPC.
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

func bridgeChat(
	ctx context.Context,
	chatClient ytgrpc.V3DataLiveChatMessageServiceClient,
	streamdClient streamd_grpc.StreamDClient,
	liveChatID string,
	hl string,
) (_err error) {
	logger.Tracef(ctx, "bridgeChat")
	defer func() { logger.Tracef(ctx, "/bridgeChat: %v", _err) }()

	var pageToken string

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		err := recvAndInject(ctx, chatClient, streamdClient, liveChatID, hl, &pageToken)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		switch {
		case err == nil:
			// Stream ended cleanly (EOF); reconnect.
			logger.Debugf(ctx, "stream ended, reconnecting...")
		default:
			logger.Warnf(ctx, "stream error, reconnecting in %s: %v", reconnectDelay, err)
		}

		if !sleep(ctx, reconnectDelay) {
			return ctx.Err()
		}
	}
}

func recvAndInject(
	ctx context.Context,
	chatClient ytgrpc.V3DataLiveChatMessageServiceClient,
	streamdClient streamd_grpc.StreamDClient,
	liveChatID string,
	hl string,
	pageToken *string,
) (_err error) {
	logger.Tracef(ctx, "recvAndInject")
	defer func() { logger.Tracef(ctx, "/recvAndInject: %v", _err) }()

	stream, err := chatClient.StreamList(ctx, &ytgrpc.LiveChatMessageListRequest{
		LiveChatId: liveChatID,
		Hl:         hl,
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
			ev := convertMessage(ctx, item)
			logger.Debugf(ctx, "injecting %s event from %s: %s",
				ev.Type, ev.User.Name, messagePreview(&ev))

			_, injectErr := streamdClient.InjectChatMessage(ctx, &streamd_grpc.InjectChatMessageRequest{
				PlatID: string(youtube.ID),
				Event:  scgoconv.EventGo2GRPC(ev),
			})
			if injectErr != nil {
				logger.Errorf(ctx, "InjectChatMessage failed for %s: %v", item.Id, injectErr)
			}
		}
	}
}

func messagePreview(ev *streamcontrol.Event) string {
	if ev.Message == nil {
		return fmt.Sprintf("(%s)", ev.Type)
	}
	content := ev.Message.Content
	const maxLen = 60
	if len(content) > maxLen {
		content = content[:maxLen] + "..."
	}
	return content
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

const tlsProbeTimeout = 3 * time.Second

// detectTransportCredentials probes the server with a TLS handshake.
// If the handshake succeeds, it returns TLS credentials (with InsecureSkipVerify
// for self-signed certs). Otherwise it returns insecure plaintext credentials.
func detectTransportCredentials(
	ctx context.Context,
	addr string,
) credentials.TransportCredentials {
	probeCtx, cancel := context.WithTimeout(ctx, tlsProbeTimeout)
	defer cancel()

	dialer := tls.Dialer{
		Config: &tls.Config{
			InsecureSkipVerify: true,
		},
	}
	conn, err := dialer.DialContext(probeCtx, "tcp", addr)
	if err != nil {
		logger.Debugf(ctx, "TLS probe to %s failed, using plaintext: %v", addr, err)
		return insecure.NewCredentials()
	}
	conn.Close()

	logger.Debugf(ctx, "TLS probe to %s succeeded, using TLS", addr)
	return credentials.NewTLS(&tls.Config{
		InsecureSkipVerify: true,
	})
}

