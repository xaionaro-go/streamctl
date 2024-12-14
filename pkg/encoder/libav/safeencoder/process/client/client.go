package client

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/encoder"
	"github.com/xaionaro-go/streamctl/pkg/encoder/libav/safeencoder/grpc/go/encoder_grpc"
	"github.com/xaionaro-go/streamctl/pkg/encoder/libav/safeencoder/grpc/goconv"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	Target string
}

func New(target string) *Client {
	return &Client{Target: target}
}

func (c *Client) grpcClient() (encoder_grpc.EncoderClient, *grpc.ClientConn, error) {
	conn, err := grpc.NewClient(
		c.Target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to initialize a gRPC client: %w", err)
	}

	client := encoder_grpc.NewEncoderClient(conn)
	return client, conn, nil
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

	_, err = client.SetLoggingLevel(ctx, &encoder_grpc.SetLoggingLevelRequest{
		Level: logLevelGo2Protobuf(logLevel),
	})
	if err != nil {
		return fmt.Errorf("query error: %w", err)
	}
	return nil
}

type InputConfig = encoder.InputConfig
type InputID uint64

func (c *Client) NewInputFromURL(
	ctx context.Context,
	url string,
	authKey string,
	config InputConfig,
) (_ InputID, _err error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	logger.Debugf(ctx, "NewInputFromURL(ctx, '%s', authKey, %#+v)", url, authKey)
	defer func() { logger.Debugf(ctx, "/NewInputFromURL(ctx, '%s', authKey, %#+v): %v", url, authKey, _err) }()

	resp, err := client.NewInput(ctx, &encoder_grpc.NewInputRequest{
		Path: &encoder_grpc.ResourcePath{
			ResourcePath: &encoder_grpc.ResourcePath_Url{
				Url: &encoder_grpc.ResourcePathURL{
					Url:     url,
					AuthKey: authKey,
				},
			},
		},
		Config: &encoder_grpc.InputConfig{},
	})
	if err != nil {
		return 0, fmt.Errorf("query error: %w", err)
	}

	return InputID(resp.GetId()), nil
}

func (c *Client) CloseInput(
	ctx context.Context,
	inputID InputID,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.CloseInput(ctx, &encoder_grpc.CloseInputRequest{
		InputID: uint64(inputID),
	})
	if err != nil {
		return fmt.Errorf("query error: %w", err)
	}

	return nil
}

type OutputID uint64
type OutputConfig = encoder.OutputConfig

func (c *Client) NewOutputFromURL(
	ctx context.Context,
	url string,
	streamKey string,
	config OutputConfig,
) (OutputID, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	resp, err := client.NewOutput(ctx, &encoder_grpc.NewOutputRequest{
		Path: &encoder_grpc.ResourcePath{
			ResourcePath: &encoder_grpc.ResourcePath_Url{
				Url: &encoder_grpc.ResourcePathURL{
					Url:     url,
					AuthKey: streamKey,
				},
			},
		},
		Config: &encoder_grpc.OutputConfig{},
	})
	if err != nil {
		return 0, fmt.Errorf("query error: %w", err)
	}

	return OutputID(resp.GetId()), nil
}

func (c *Client) CloseOutput(
	ctx context.Context,
	outputID OutputID,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.CloseOutput(ctx, &encoder_grpc.CloseOutputRequest{
		OutputID: uint64(outputID),
	})
	if err != nil {
		return fmt.Errorf("query error: %w", err)
	}

	return nil
}

type EncoderID uint64
type EncoderConfig = encoder.Config

func (c *Client) NewEncoder(
	ctx context.Context,
) (EncoderID, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	resp, err := client.NewEncoder(ctx, &encoder_grpc.NewEncoderRequest{})
	if err != nil {
		return 0, fmt.Errorf("query error: %w", err)
	}

	return EncoderID(resp.GetId()), nil
}

func (c *Client) SetEncoderConfig(
	ctx context.Context,
	config EncoderConfig,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.SetEncoderConfig(ctx, &encoder_grpc.SetEncoderConfigRequest{
		Config: goconv.EncoderConfigToThrift(config),
	})
	if err != nil {
		return fmt.Errorf("query error: %w", err)
	}

	return nil
}

func (c *Client) StartEncoding(
	ctx context.Context,
	recoderID EncoderID,
	inputID InputID,
	outputID OutputID,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.StartEncoding(ctx, &encoder_grpc.StartEncodingRequest{
		EncoderID: uint64(recoderID),
		InputID:   uint64(inputID),
		OutputID:  uint64(outputID),
	})
	if err != nil {
		return fmt.Errorf("query error: %w", err)
	}

	return nil
}

type EncoderStats = encoder.Stats

func (c *Client) GetEncoderStats(
	ctx context.Context,
	recoderID EncoderID,
) (*EncoderStats, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	resp, err := client.GetEncoderStats(ctx, &encoder_grpc.GetEncoderStatsRequest{
		EncoderID: uint64(recoderID),
	})
	if err != nil {
		return nil, fmt.Errorf("query error: %w", err)
	}

	return &EncoderStats{
		BytesCountRead:  resp.GetBytesCountRead(),
		BytesCountWrote: resp.GetBytesCountWrote(),
	}, nil
}

func (c *Client) EncodingEndedChan(
	ctx context.Context,
	recoderID EncoderID,
) (<-chan struct{}, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return nil, err
	}

	waiter, err := client.EncodingEndedChan(ctx, &encoder_grpc.EncodingEndedChanRequest{
		EncoderID: uint64(recoderID),
	})
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
