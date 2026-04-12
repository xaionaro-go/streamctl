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
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const grpcReconnectDelay = 5 * time.Second

// GRPCStreamListener implements chathandler.ChatListener using youtubeapiproxy's
// gRPC StreamList RPC. It auto-discovers active broadcasts via the admin RPCs
// and reconnects when a stream ends or a new one starts.
type GRPCStreamListener struct {
	YTProxyAddr  string
	ChannelID    string
	DetectMethod DetectMethod

	conn   *grpc.ClientConn
	cancel context.CancelFunc
}

func (l *GRPCStreamListener) Name() string { return "YouTube-gRPC" }

func (l *GRPCStreamListener) Listen(
	ctx context.Context,
) (_ <-chan streamcontrol.Event, _err error) {
	logger.Tracef(ctx, "GRPCStreamListener.Listen")
	defer func() { logger.Tracef(ctx, "/GRPCStreamListener.Listen: %v", _err) }()

	conn, err := grpc.NewClient(l.YTProxyAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("connect to youtubeapiproxy at %s: %w", l.YTProxyAddr, err)
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
		// Default: broadcasts (mine=true) requires least quota and
		// sees private/unlisted streams.
		detectMethod = DetectMethodBroadcasts
	}

	chatClient := ytgrpc.NewV3DataLiveChatMessageServiceClient(conn)

	for {
		if ctx.Err() != nil {
			return
		}

		logger.Debugf(ctx, "discovering active broadcast...")
		result, err := DiscoverBroadcast(ctx, conn, l.ChannelID, detectMethod)
		if err != nil {
			// DiscoverBroadcast only returns on ctx cancellation.
			logger.Debugf(ctx, "broadcast discovery ended: %v", err)
			return
		}

		logger.Debugf(ctx, "streaming chat for liveChatID=%s (video=%s)", result.LiveChatID, result.VideoID)
		l.streamChat(ctx, chatClient, result.LiveChatID, ch)
		logger.Debugf(ctx, "chat stream ended, will re-discover")
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
