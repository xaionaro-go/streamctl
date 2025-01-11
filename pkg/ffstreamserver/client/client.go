package client

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/libsrt"
	"github.com/xaionaro-go/observability"
	ffstreamtypes "github.com/xaionaro-go/streamctl/pkg/ffstream/types"
	"github.com/xaionaro-go/streamctl/pkg/ffstreamserver/grpc/go/ffstream_grpc"
	"github.com/xaionaro-go/streamctl/pkg/ffstreamserver/grpc/goconv"
	recodertypes "github.com/xaionaro-go/streamctl/pkg/recoder/libav/recoder/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	Target string
}

func New(target string) *Client {
	return &Client{Target: target}
}

func (c *Client) grpcClient() (ffstream_grpc.FFStreamClient, *grpc.ClientConn, error) {
	conn, err := grpc.NewClient(
		c.Target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to initialize a gRPC client: %w", err)
	}

	client := ffstream_grpc.NewFFStreamClient(conn)
	return client, conn, nil
}

func logLevelGo2Protobuf(logLevel logger.Level) ffstream_grpc.LoggingLevel {
	switch logLevel {
	case logger.LevelFatal:
		return ffstream_grpc.LoggingLevel_LoggingLevelFatal
	case logger.LevelPanic:
		return ffstream_grpc.LoggingLevel_LoggingLevelPanic
	case logger.LevelError:
		return ffstream_grpc.LoggingLevel_LoggingLevelError
	case logger.LevelWarning:
		return ffstream_grpc.LoggingLevel_LoggingLevelWarn
	case logger.LevelInfo:
		return ffstream_grpc.LoggingLevel_LoggingLevelInfo
	case logger.LevelDebug:
		return ffstream_grpc.LoggingLevel_LoggingLevelDebug
	case logger.LevelTrace:
		return ffstream_grpc.LoggingLevel_LoggingLevelTrace
	default:
		return ffstream_grpc.LoggingLevel_LoggingLevelWarn
	}
}

func (c *Client) SetLoggingLevel(
	ctx context.Context,
	logLevel logger.Level,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.SetLoggingLevel(ctx, &ffstream_grpc.SetLoggingLevelRequest{
		Level: logLevelGo2Protobuf(logLevel),
	})
	if err != nil {
		return fmt.Errorf("query error: %w", err)
	}
	return nil
}

func (c *Client) AddInput(
	ctx context.Context,
	url string,
	customOptions []recodertypes.CustomOption,
) (_ recodertypes.InputID, _err error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	logger.Debugf(ctx, "AddInput(ctx, '%s', %#+v)", url, customOptions)
	defer func() { logger.Debugf(ctx, "/AddInput(ctx, '%s', %#+v): %v", url, customOptions, _err) }()

	resp, err := client.AddInput(ctx, &ffstream_grpc.AddInputRequest{
		Url:           url,
		CustomOptions: goconv.CustomOptionsToGRPC(customOptions),
	})
	if err != nil {
		return 0, fmt.Errorf("query error: %w", err)
	}

	return recodertypes.InputID(resp.GetId()), nil
}

func (c *Client) AddOutput(
	ctx context.Context,
	url string,
	customOptions []recodertypes.CustomOption,
) (recodertypes.OutputID, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	resp, err := client.AddOutput(ctx, &ffstream_grpc.AddOutputRequest{
		Url:           url,
		CustomOptions: goconv.CustomOptionsToGRPC(customOptions),
	})
	if err != nil {
		return 0, fmt.Errorf("query error: %w", err)
	}

	return recodertypes.OutputID(resp.GetId()), nil
}

func (c *Client) RemoveOutput(
	ctx context.Context,
	outputID recodertypes.OutputID,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.RemoveOutput(ctx, &ffstream_grpc.RemoveOutputRequest{
		Id: uint64(outputID),
	})
	if err != nil {
		return fmt.Errorf("query error: %w", err)
	}

	return nil
}

func (c *Client) GetEncoderConfig(
	ctx context.Context,
) (*ffstreamtypes.EncoderConfig, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	resp, err := client.GetEncoderConfig(ctx, &ffstream_grpc.GetEncoderConfigRequest{})
	if err != nil {
		return nil, fmt.Errorf("query error: %w", err)
	}

	return ptr(goconv.EncoderConfigFromGRPC(resp.GetConfig())), nil
}

func (c *Client) SetEncoderConfig(
	ctx context.Context,
	cfg ffstreamtypes.EncoderConfig,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.SetEncoderConfig(ctx, &ffstream_grpc.SetEncoderConfigRequest{
		Config: goconv.EncoderConfigToGRPC(cfg),
	})
	if err != nil {
		return fmt.Errorf("query error: %w", err)
	}

	return nil
}

func (c *Client) Start(
	ctx context.Context,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.Start(ctx, &ffstream_grpc.StartRequest{})
	if err != nil {
		return fmt.Errorf("query error: %w", err)
	}

	return nil
}

func (c *Client) End(
	ctx context.Context,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.End(ctx, &ffstream_grpc.EndRequest{})
	if err != nil {
		return fmt.Errorf("query error: %w", err)
	}

	return nil
}

func (c *Client) GetEncoderStats(
	ctx context.Context,
) (*recodertypes.EncoderStatistics, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	resp, err := client.GetEncoderStats(ctx, &ffstream_grpc.GetEncoderStatsRequest{})
	if err != nil {
		return nil, fmt.Errorf("query error: %w", err)
	}

	return goconv.EncoderStatsFromGRPC(resp), nil
}

func (c *Client) GetOutputSRTStats(
	ctx context.Context,
) (*libsrt.Tracebstats, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	resp, err := client.GetOutputSRTStats(ctx, &ffstream_grpc.GetOutputSRTStatsRequest{})
	if err != nil {
		return nil, fmt.Errorf("query error: %w", err)
	}

	return goconv.OutputSRTStatsFromGRPC(resp), nil
}

func (c *Client) GetFlagInt(
	ctx context.Context,
	flag libsrt.Sockopt,
) (int64, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	flagID := goconv.SockoptIntToGRPC(flag)
	if flagID == ffstream_grpc.FlagInt_undefined {
		return 0, fmt.Errorf("unknown flag: %v", flag)
	}

	resp, err := client.GetFlagInt(ctx, &ffstream_grpc.GetFlagIntRequest{
		Flag: flagID,
	})
	if err != nil {
		return 0, fmt.Errorf("query error: %w", err)
	}

	return resp.GetValue(), nil
}

func (c *Client) SetFlagInt(
	ctx context.Context,
	flag libsrt.Sockopt,
	value int64,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	flagID := goconv.SockoptIntToGRPC(flag)
	if flagID == ffstream_grpc.FlagInt_undefined {
		return fmt.Errorf("unknown flag: %v", flag)
	}

	_, err = client.SetFlagInt(ctx, &ffstream_grpc.SetFlagIntRequest{
		Flag:  flagID,
		Value: value,
	})
	if err != nil {
		return fmt.Errorf("query error: %w", err)
	}

	return nil
}

func (c *Client) WaitChan(
	ctx context.Context,
) (<-chan struct{}, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return nil, err
	}

	waiter, err := client.WaitChan(ctx, &ffstream_grpc.WaitRequest{})
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
		if err != nil && !errors.Is(err, context.Canceled) {
			logger.Errorf(ctx, "unable to read data: %v", err)
			return
		}
	})

	return result, nil
}
