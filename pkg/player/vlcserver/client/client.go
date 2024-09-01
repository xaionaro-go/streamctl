//go:build with_libvlc
// +build with_libvlc

package client

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/player/protobuf/go/player_grpc"
	"github.com/xaionaro-go/streamctl/pkg/player/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	Title  string
	Target string
}

var _ types.Player = (*Client)(nil)

func New(title, target string) *Client {
	return &Client{Title: title, Target: target}
}

func (c *Client) grpcClient() (player_grpc.PlayerClient, *grpc.ClientConn, error) {
	conn, err := grpc.NewClient(
		c.Target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to initialize a gRPC client: %w", err)
	}

	client := player_grpc.NewPlayerClient(conn)
	return client, conn, nil
}

func (c *Client) ProcessTitle(
	ctx context.Context,
) (string, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return "", err
	}
	defer conn.Close()

	req, err := client.ProcessTitle(ctx, &player_grpc.ProcessTitleRequest{})
	if err != nil {
		return "", fmt.Errorf("query error: %w", err)
	}
	return req.GetTitle(), nil
}

func logLevelGo2Protobuf(logLevel logger.Level) player_grpc.LoggingLevel {
	switch logLevel {
	case logger.LevelFatal:
		return player_grpc.LoggingLevel_LoggingLevelFatal
	case logger.LevelPanic:
		return player_grpc.LoggingLevel_LoggingLevelPanic
	case logger.LevelError:
		return player_grpc.LoggingLevel_LoggingLevelError
	case logger.LevelWarning:
		return player_grpc.LoggingLevel_LoggingLevelWarn
	case logger.LevelInfo:
		return player_grpc.LoggingLevel_LoggingLevelInfo
	case logger.LevelDebug:
		return player_grpc.LoggingLevel_LoggingLevelDebug
	case logger.LevelTrace:
		return player_grpc.LoggingLevel_LoggingLevelTrace
	default:
		return player_grpc.LoggingLevel_LoggingLevelWarn
	}
}

func (c *Client) SetupForStreaming(
	ctx context.Context,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.SetupForStreaming(ctx, &player_grpc.SetupForStreamingRequest{})
	if err != nil {
		return fmt.Errorf("query error: %w", err)
	}
	return nil
}

func (c *Client) OpenURL(
	ctx context.Context,
	link string,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.Open(ctx, &player_grpc.OpenRequest{
		Link:         link,
		Title:        c.Title,
		LoggingLevel: logLevelGo2Protobuf(logger.FromCtx(ctx).Level()),
	})
	if err != nil {
		return fmt.Errorf("query error: %w", err)
	}
	return nil
}

func (c *Client) GetLink(
	ctx context.Context,
) (string, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return "", err
	}
	defer conn.Close()

	req, err := client.GetLink(ctx, &player_grpc.GetLinkRequest{})
	if err != nil {
		return "", fmt.Errorf("query error: %w", err)
	}
	return req.GetLink(), nil
}

func (c *Client) EndChan(ctx context.Context) (<-chan struct{}, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return nil, err
	}

	waiter, err := client.EndChan(ctx, &player_grpc.EndChanRequest{})
	if err != nil {
		return nil, fmt.Errorf("query error: %w", err)
	}

	result := make(chan struct{})
	waiter.CloseSend()
	observability.Go(ctx, func() {
		defer conn.Close()
		defer func() {
			close(result)
		}()

		_, err := waiter.Recv()
		if err == io.EOF {
			logger.Debugf(ctx, "the receiver is closed: %v", err)
			return
		}
		if err != nil {
			logger.Errorf(ctx, "unable to read data: %v", err)
			return
		}
	})

	return result, nil
}

func (c *Client) IsEnded(
	ctx context.Context,
) (bool, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return false, err
	}
	defer conn.Close()

	req, err := client.IsEnded(ctx, &player_grpc.IsEndedRequest{})
	if err != nil {
		return false, fmt.Errorf("query error: %w", err)
	}
	return req.GetIsEnded(), nil
}

func (c *Client) GetPosition(
	ctx context.Context,
) (time.Duration, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	req, err := client.GetPosition(ctx, &player_grpc.GetPositionRequest{})
	if err != nil {
		return 0, fmt.Errorf("query error: %w", err)
	}
	return time.Duration(req.GetPositionSecs() * float64(time.Second)), nil
}
func (c *Client) GetLength(
	ctx context.Context,
) (time.Duration, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	req, err := client.GetLength(ctx, &player_grpc.GetLengthRequest{})
	if err != nil {
		return 0, fmt.Errorf("query error: %w", err)
	}
	return time.Duration(req.GetLengthSecs() * float64(time.Second)), nil
}
func (c *Client) SetSpeed(
	ctx context.Context,
	speed float64,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.SetSpeed(ctx, &player_grpc.SetSpeedRequest{Speed: speed})
	if err != nil {
		return fmt.Errorf("query error: %w", err)
	}
	return nil
}
func (c *Client) SetPause(
	ctx context.Context,
	pause bool,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.SetPause(ctx, &player_grpc.SetPauseRequest{
		SetPaused: pause,
	})
	if err != nil {
		return fmt.Errorf("query error: %w", err)
	}
	return nil
}
func (c *Client) Stop(
	ctx context.Context,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.Stop(ctx, &player_grpc.StopRequest{})
	if err != nil {
		return fmt.Errorf("query error: %w", err)
	}
	return nil
}
func (c *Client) Close(ctx context.Context) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.Close(ctx, &player_grpc.CloseRequest{})
	if err != nil {
		return fmt.Errorf("query error: %w", err)
	}
	return nil
}
