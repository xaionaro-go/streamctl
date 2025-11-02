package client

import (
	"context"
	"fmt"
	"io"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick/chathandlerobsolete/protobuf/go/chathandlerobsolete_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/protobuf/go/streamcontrol_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/protobuf/goconv"
	"github.com/xaionaro-go/xgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type GRPCClient struct {
	Target string
}

func New(
	ctx context.Context,
	target string,
	channelSlug string,
) (*GRPCClient, error) {
	c := &GRPCClient{
		Target: target,
	}
	err := c.open(ctx, channelSlug)
	if err != nil {
		return nil, fmt.Errorf("unable to open the chat handler: %w", err)
	}
	return c, nil
}

func (c *GRPCClient) GRPCClient(
	ctx context.Context,
) (chathandlerobsolete_grpc.ChatHandlerObsoleteClient, io.Closer, error) {
	conn, err := grpc.NewClient(
		c.Target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to initialize a gRPC client: %w", err)
	}

	client := chathandlerobsolete_grpc.NewChatHandlerObsoleteClient(conn)
	return client, conn, nil
}

func (c *GRPCClient) ProcessError(ctx context.Context, err error) error {
	logger.Errorf(ctx, "gRPC call error: %v", err)
	return err
}

func getCommonsFromCtx(ctx context.Context) *chathandlerobsolete_grpc.RequestCommons {
	logLevel := logger.FromCtx(ctx).Level()
	return &chathandlerobsolete_grpc.RequestCommons{
		LoggingLevel: logLevelGo2Protobuf(logLevel),
	}
}

func (c *GRPCClient) open(
	ctx context.Context,
	channelSlug string,
) error {
	grpcClient, conn, err := c.GRPCClient(ctx)
	if err != nil {
		return fmt.Errorf("unable to initialize a gRPC client: %w", err)
	}
	defer conn.Close()

	_, err = grpcClient.Open(ctx, &chathandlerobsolete_grpc.OpenRequest{
		Commons:     getCommonsFromCtx(ctx),
		ChannelSlug: channelSlug,
	})
	if err != nil {
		return fmt.Errorf("unable to open the chat handler: %w", err)
	}

	return nil
}

func logLevelGo2Protobuf(logLevel logger.Level) chathandlerobsolete_grpc.LoggingLevel {
	switch logLevel {
	case logger.LevelFatal:
		return chathandlerobsolete_grpc.LoggingLevel_LoggingLevelFatal
	case logger.LevelPanic:
		return chathandlerobsolete_grpc.LoggingLevel_LoggingLevelPanic
	case logger.LevelError:
		return chathandlerobsolete_grpc.LoggingLevel_LoggingLevelError
	case logger.LevelWarning:
		return chathandlerobsolete_grpc.LoggingLevel_LoggingLevelWarn
	case logger.LevelInfo:
		return chathandlerobsolete_grpc.LoggingLevel_LoggingLevelInfo
	case logger.LevelDebug:
		return chathandlerobsolete_grpc.LoggingLevel_LoggingLevelDebug
	case logger.LevelTrace:
		return chathandlerobsolete_grpc.LoggingLevel_LoggingLevelTrace
	default:
		return chathandlerobsolete_grpc.LoggingLevel_LoggingLevelWarn
	}
}

func (c *GRPCClient) GetCallWrapper() xgrpc.CallWrapperFunc {
	return nil
}

func (c *GRPCClient) GetMessagesChan(
	ctx context.Context,
) (<-chan streamcontrol.Event, error) {
	return xgrpc.UnwrapChan(
		ctx,
		c,
		func(
			ctx context.Context,
			client chathandlerobsolete_grpc.ChatHandlerObsoleteClient,
		) (chathandlerobsolete_grpc.ChatHandlerObsolete_MessagesChanClient, error) {
			return xgrpc.Call(
				ctx,
				c,
				client.MessagesChan,
				&chathandlerobsolete_grpc.MessagesChanRequest{
					Commons: getCommonsFromCtx(ctx),
				},
			)
		},
		func(
			ctx context.Context,
			event *streamcontrol_grpc.Event,
		) streamcontrol.Event {
			return goconv.EventGRPC2Go(event)
		},
	)
}

func (c *GRPCClient) Close(ctx context.Context) error {
	grpcClient, conn, err := c.GRPCClient(ctx)
	if err != nil {
		return fmt.Errorf("unable to initialize a gRPC client: %w", err)
	}
	defer conn.Close()

	_, err = grpcClient.Close(ctx, &chathandlerobsolete_grpc.CloseRequest{
		Commons: getCommonsFromCtx(ctx),
	})
	if err != nil {
		return fmt.Errorf("unable to close the chat handler: %w", err)
	}

	return nil
}
