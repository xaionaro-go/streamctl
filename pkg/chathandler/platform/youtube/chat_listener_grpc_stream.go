package youtube

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/youtubeapiproxy/grpc/ytgrpc"
	"golang.org/x/oauth2"
	youtubesvc "google.golang.org/api/youtube/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	grpcoauth "google.golang.org/grpc/credentials/oauth"
)

const (
	grpcReconnectDelay = 5 * time.Second

	// directGRPCHost is the YouTube Data API gRPC endpoint used when
	// connecting directly (without youtubeapiproxy).
	directGRPCHost = "youtube.googleapis.com:443"
)

// GRPCStreamListener implements chathandler.ChatListener using the YouTube
// gRPC StreamList RPC. It supports two connection modes:
//
//   - Proxy mode (YTProxyAddr set): connects to youtubeapiproxy with insecure
//     credentials. Broadcast discovery uses the proxy's AdminService RPCs.
//   - Direct mode (YTProxyAddr empty): connects to youtube.googleapis.com:443
//     with TLS and OAuth2 per-RPC credentials. Broadcast discovery uses the
//     YouTube Data API v3 via OAuth2.
type GRPCStreamListener struct {
	YTProxyAddr  string
	ChannelID    string
	DetectMethod DetectMethod

	// Direct mode fields (used when YTProxyAddr is empty).
	TokenSource oauth2.TokenSource
	OAuth2Svc   *youtubesvc.Service

	conn   *grpc.ClientConn
	cancel context.CancelFunc
}

func (l *GRPCStreamListener) Name() string { return "YouTube-gRPC" }

func (l *GRPCStreamListener) isDirectMode() bool {
	return l.YTProxyAddr == ""
}

func (l *GRPCStreamListener) Listen(
	ctx context.Context,
) (_ <-chan streamcontrol.Event, _err error) {
	logger.Tracef(ctx, "GRPCStreamListener.Listen")
	defer func() { logger.Tracef(ctx, "/GRPCStreamListener.Listen: %v", _err) }()

	conn, err := l.dial(ctx)
	if err != nil {
		return nil, err
	}
	l.conn = conn

	listenCtx, cancel := context.WithCancel(ctx)
	l.cancel = cancel

	ch := make(chan streamcontrol.Event, 64)

	observability.Go(ctx, func(ctx context.Context) {
		defer close(ch)
		l.discoverAndStreamLoop(listenCtx, conn, ch)
	})

	return ch, nil
}

// dial creates the gRPC connection appropriate for the current mode.
func (l *GRPCStreamListener) dial(
	ctx context.Context,
) (_ *grpc.ClientConn, _err error) {
	logger.Tracef(ctx, "GRPCStreamListener.dial")
	defer func() { logger.Tracef(ctx, "/GRPCStreamListener.dial: %v", _err) }()

	switch {
	case !l.isDirectMode():
		logger.Debugf(ctx, "connecting to youtubeapiproxy at %s", l.YTProxyAddr)
		conn, err := grpc.NewClient(l.YTProxyAddr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			return nil, fmt.Errorf("connect to youtubeapiproxy at %s: %w", l.YTProxyAddr, err)
		}
		return conn, nil

	default:
		logger.Debugf(ctx, "connecting directly to %s with OAuth2 credentials", directGRPCHost)
		conn, err := grpc.NewClient(directGRPCHost,
			grpc.WithTransportCredentials(credentials.NewTLS(nil)),
			grpc.WithPerRPCCredentials(&grpcoauth.TokenSource{TokenSource: l.TokenSource}),
		)
		if err != nil {
			return nil, fmt.Errorf("connect to %s: %w", directGRPCHost, err)
		}
		return conn, nil
	}
}

// discoverAndStreamLoop continuously discovers broadcasts and streams chat
// messages. When a stream ends, it re-discovers the next active broadcast.
func (l *GRPCStreamListener) discoverAndStreamLoop(
	ctx context.Context,
	conn *grpc.ClientConn,
	ch chan<- streamcontrol.Event,
) {
	logger.Tracef(ctx, "discoverAndStreamLoop")
	defer func() { logger.Tracef(ctx, "/discoverAndStreamLoop") }()

	detectMethod := l.DetectMethod
	if detectMethod == "" {
		detectMethod = DetectMethodBroadcasts
	}

	chatClient := ytgrpc.NewV3DataLiveChatMessageServiceClient(conn)

	for {
		if ctx.Err() != nil {
			return
		}

		logger.Debugf(ctx, "discovering active broadcast...")
		result, err := l.discoverBroadcast(ctx, conn, detectMethod)
		if err != nil {
			logger.Debugf(ctx, "broadcast discovery ended: %v", err)
			return
		}

		logger.Debugf(ctx, "streaming chat for liveChatID=%s (video=%s)", result.LiveChatID, result.VideoID)
		l.streamChat(ctx, chatClient, result.LiveChatID, ch)
		logger.Debugf(ctx, "chat stream ended, will re-discover")
	}
}

// discoverBroadcast selects the appropriate broadcast discovery strategy
// based on the connection mode.
func (l *GRPCStreamListener) discoverBroadcast(
	ctx context.Context,
	conn *grpc.ClientConn,
	detectMethod DetectMethod,
) (_ BroadcastResult, _err error) {
	logger.Tracef(ctx, "discoverBroadcast")
	defer func() { logger.Tracef(ctx, "/discoverBroadcast: %v", _err) }()

	switch {
	case !l.isDirectMode():
		// Proxy mode: use AdminService RPCs via the proxy connection.
		return DiscoverBroadcast(ctx, conn, l.ChannelID, detectMethod)

	default:
		// Direct mode: use YouTube Data API v3 via OAuth2.
		return DiscoverBroadcastViaOAuth2(ctx, l.OAuth2Svc)
	}
}

// streamChat streams messages for a single liveChatID. Returns when the chat
// ends or an unrecoverable error occurs.
func (l *GRPCStreamListener) streamChat(
	ctx context.Context,
	chatClient ytgrpc.V3DataLiveChatMessageServiceClient,
	liveChatID string,
	ch chan<- streamcontrol.Event,
) {
	logger.Tracef(ctx, "streamChat(%s)", liveChatID)
	defer func() { logger.Tracef(ctx, "/streamChat(%s)", liveChatID) }()

	for {
		if ctx.Err() != nil {
			return
		}

		err := l.recvBatch(ctx, chatClient, liveChatID, ch)
		if ctx.Err() != nil {
			return
		}

		switch {
		case err == nil:
			// Stream ended cleanly (EOF) -- reconnect immediately to
			// minimize latency between message batches.
			logger.Debugf(ctx, "stream EOF, reconnecting immediately")
			continue
		case isStreamEndedError(err):
			// Live chat no longer exists (stream ended).
			logger.Debugf(ctx, "live chat ended: %v", err)
			return
		default:
			logger.Warnf(ctx, "stream error, reconnecting in %s: %v", grpcReconnectDelay, err)
			if !sleepCtx(ctx, grpcReconnectDelay) {
				return
			}
		}
	}
}

// recvBatch opens one StreamList call and forwards messages to ch.
func (l *GRPCStreamListener) recvBatch(
	ctx context.Context,
	chatClient ytgrpc.V3DataLiveChatMessageServiceClient,
	liveChatID string,
	ch chan<- streamcontrol.Event,
) error {
	stream, err := chatClient.StreamList(ctx, &ytgrpc.LiveChatMessageListRequest{
		LiveChatId: liveChatID,
		Part:       []string{"snippet", "authorDetails"},
	})
	if err != nil {
		return fmt.Errorf("StreamList: %w", err)
	}

	for {
		resp, err := stream.Recv()
		switch {
		case err == io.EOF:
			return nil
		case err != nil:
			return fmt.Errorf("stream recv: %w", err)
		}

		for _, item := range resp.Items {
			ev := ConvertGRPCMessage(ctx, item)
			select {
			case ch <- ev:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

// isStreamEndedError returns true if the error indicates the live chat
// no longer exists (stream ended, chat disabled, etc.).
func isStreamEndedError(err error) bool {
	s := err.Error()
	switch {
	case strings.Contains(s, "FailedPrecondition"):
		return true
	case strings.Contains(s, "liveChatEnded"):
		return true
	case strings.Contains(s, "liveChatNotFound"):
		return true
	case strings.Contains(s, "liveChatDisabled"):
		return true
	default:
		return false
	}
}

func (l *GRPCStreamListener) Close(_ context.Context) error {
	if l.cancel != nil {
		l.cancel()
		l.cancel = nil
	}

	if l.conn != nil {
		err := l.conn.Close()
		l.conn = nil
		return err
	}

	return nil
}
