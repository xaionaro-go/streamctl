package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/youtubeapiproxy/grpc/ytgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	platformIDYouTube = "youtube"
	reconnectDelay    = 5 * time.Second
)

func main() {
	ytProxyAddr := pflag.String("yt-proxy-addr", "", "youtubeapiproxy gRPC address (host:port)")
	streamdAddr := pflag.String("streamd-addr", "", "streamd gRPC address (host:port)")
	video := pflag.String("video", "", "video URL, video ID, or liveChatId")
	channel := pflag.String("channel", "", "channel URL, @handle, or channel ID to monitor for live streams")
	hl := pflag.String("hl", "", "language for YouTube system messages (e.g. en, de, ja)")
	useRawMessage := pflag.Bool("raw-message", false, "use TextMessageDetails instead of DisplayMessage")
	var logLevel logger.Level
	pflag.Var(&logLevel, "log-level", "log level")
	pflag.Parse()

	if logLevel == logger.LevelUndefined {
		logLevel = logger.LevelWarning
	}
	observability.LogLevelFilter.SetLevel(logLevel)

	l := logrus.Default()
	ctx := logger.CtxWithLogger(context.Background(), l)
	ctx = belt.CtxWithBelt(ctx, belt.New())
	ctx = logger.CtxWithLogger(ctx, l.WithLevel(logLevel))
	defer belt.Flush(ctx)

	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()

	if err := run(ctx, *ytProxyAddr, *streamdAddr, *video, *channel, *hl, *useRawMessage); err != nil {
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
	useRawMessage bool,
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

	ytClient := ytgrpc.NewV3DataLiveChatMessageServiceClient(ytConn)
	sdClient := streamd_grpc.NewStreamDClient(sdConn)

	if channel != "" {
		return monitorChannel(ctx, ytConn, channel, func(ctx context.Context, liveChatID string) error {
			return bridgeLoop(ctx, ytClient, sdClient, liveChatID, hl, useRawMessage)
		})
	}

	liveChatID, err := resolveLiveChatID(ctx, ytConn, video)
	if err != nil {
		return fmt.Errorf("resolve live chat ID for %q: %w", video, err)
	}
	logger.Infof(ctx, "resolved live chat ID: %s", liveChatID)

	return bridgeLoop(ctx, ytClient, sdClient, liveChatID, hl, useRawMessage)
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

func bridgeLoop(
	ctx context.Context,
	ytClient ytgrpc.V3DataLiveChatMessageServiceClient,
	sdClient streamd_grpc.StreamDClient,
	liveChatID string,
	hl string,
	useRawMessage bool,
) (_err error) {
	logger.Tracef(ctx, "bridgeLoop")
	defer func() { logger.Tracef(ctx, "/bridgeLoop: %v", _err) }()

	var nextPageToken string

	for {
		err := streamOnce(ctx, ytClient, sdClient, liveChatID, hl, useRawMessage, &nextPageToken)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		switch {
		case err == nil:
			logger.Debugf(ctx, "stream ended, reconnecting...")
		default:
			logger.Warnf(ctx, "stream error, retrying in %v: %v", reconnectDelay, err)
		}

		if !sleep(ctx, reconnectDelay) {
			return ctx.Err()
		}
	}
}

func streamOnce(
	ctx context.Context,
	ytClient ytgrpc.V3DataLiveChatMessageServiceClient,
	sdClient streamd_grpc.StreamDClient,
	liveChatID string,
	hl string,
	useRawMessage bool,
	nextPageToken *string,
) (_err error) {
	logger.Tracef(ctx, "streamOnce")
	defer func() { logger.Tracef(ctx, "/streamOnce: %v", _err) }()

	req := &ytgrpc.LiveChatMessageListRequest{
		LiveChatId: liveChatID,
		Hl:         hl,
		Part:       []string{"snippet", "authorDetails"},
		PageToken:  *nextPageToken,
	}

	stream, err := ytClient.StreamList(ctx, req)
	if err != nil {
		return fmt.Errorf("unable to start StreamList: %w", err)
	}

	for {
		resp, err := stream.Recv()
		if err != nil {
			return fmt.Errorf("stream recv: %w", err)
		}

		if token := resp.GetNextPageToken(); token != "" {
			*nextPageToken = token
		}

		for _, msg := range resp.GetItems() {
			if err := injectMessage(ctx, sdClient, msg, useRawMessage); err != nil {
				logger.Errorf(ctx, "unable to inject message %s: %v", msg.GetId(), err)
			}
		}
	}
}

func injectMessage(
	ctx context.Context,
	sdClient streamd_grpc.StreamDClient,
	msg *ytgrpc.LiveChatMessage,
	useRawMessage bool,
) error {
	ev, err := convertLiveChatMessage(msg, useRawMessage)
	if err != nil {
		return fmt.Errorf("unable to convert message: %w", err)
	}

	logger.Debugf(ctx, "injecting message id=%s user=%s: %s",
		ev.GetId(), ev.GetUser().GetName(), ev.GetMessage().GetContent())

	_, err = sdClient.InjectPlatformEvent(ctx, &streamd_grpc.InjectPlatformEventRequest{
		PlatID:       platformIDYouTube,
		IsLive:       true,
		IsPersistent: true,
		Message:      ev,
	})
	if err != nil {
		return fmt.Errorf("InjectPlatformEvent: %w", err)
	}
	return nil
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
