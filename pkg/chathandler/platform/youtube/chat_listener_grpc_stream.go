package youtube

import (
	"context"
	"fmt"
	"io"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/youtubeapiproxy/grpc/ytgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// GRPCStreamListener implements chathandler.ChatListener using youtubeapiproxy's
// gRPC StreamList RPC. This provides server-pushed chat messages instead of polling.
type GRPCStreamListener struct {
	YTProxyAddr string
	LiveChatID  string
	conn        *grpc.ClientConn
	cancel      context.CancelFunc
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

	chatClient := ytgrpc.NewV3DataLiveChatMessageServiceClient(conn)

	streamCtx, cancel := context.WithCancel(ctx)
	l.cancel = cancel

	stream, err := chatClient.StreamList(streamCtx, &ytgrpc.LiveChatMessageListRequest{
		LiveChatId: l.LiveChatID,
		Part:       []string{"snippet", "authorDetails"},
	})
	if err != nil {
		cancel()
		conn.Close()
		return nil, fmt.Errorf("StreamList RPC: %w", err)
	}

	ch := make(chan streamcontrol.Event, 64)

	observability.Go(ctx, func(ctx context.Context) {
		defer close(ch)
		receiveLoop(ctx, stream, ch)
	})

	return ch, nil
}

func receiveLoop(
	ctx context.Context,
	stream ytgrpc.V3DataLiveChatMessageService_StreamListClient,
	ch chan<- streamcontrol.Event,
) {
	for {
		resp, err := stream.Recv()
		switch {
		case err == io.EOF:
			logger.Debugf(ctx, "gRPC stream ended (EOF)")
			return
		case err != nil:
			logger.Warnf(ctx, "gRPC stream recv error: %v", err)
			return
		}

		for _, item := range resp.Items {
			ev := ConvertGRPCMessage(ctx, item)
			select {
			case ch <- ev:
			case <-ctx.Done():
				return
			}
		}
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
