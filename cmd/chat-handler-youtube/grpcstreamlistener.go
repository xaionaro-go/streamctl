package main

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

// grpcStreamListener implements chathandler.ChatListener using youtubeapiproxy's
// gRPC StreamList RPC. This provides server-pushed chat messages instead of polling.
type grpcStreamListener struct {
	ytProxyAddr string
	liveChatID  string
	conn        *grpc.ClientConn
	cancel      context.CancelFunc
}

func (l *grpcStreamListener) Name() string { return "YouTube-gRPC" }

func (l *grpcStreamListener) Listen(
	ctx context.Context,
) (<-chan streamcontrol.Event, error) {
	conn, err := grpc.NewClient(l.ytProxyAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("connect to youtubeapiproxy at %s: %w", l.ytProxyAddr, err)
	}
	l.conn = conn

	chatClient := ytgrpc.NewV3DataLiveChatMessageServiceClient(conn)

	streamCtx, cancel := context.WithCancel(ctx)
	l.cancel = cancel

	stream, err := chatClient.StreamList(streamCtx, &ytgrpc.LiveChatMessageListRequest{
		LiveChatId: l.liveChatID,
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
		l.receiveLoop(ctx, stream, ch)
	})

	return ch, nil
}

func (l *grpcStreamListener) receiveLoop(
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
			ev := convertGRPCMessage(ctx, item)
			select {
			case ch <- ev:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (l *grpcStreamListener) Close(_ context.Context) error {
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
