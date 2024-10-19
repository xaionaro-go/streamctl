package client

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/recoder"
	"github.com/xaionaro-go/streamctl/pkg/recoder/libav/saferecoder/grpc/go/recoder_grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	Target string
}

func New(target string) *Client {
	return &Client{Target: target}
}

func (c *Client) grpcClient() (recoder_grpc.RecoderClient, *grpc.ClientConn, error) {
	conn, err := grpc.NewClient(
		c.Target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to initialize a gRPC client: %w", err)
	}

	client := recoder_grpc.NewRecoderClient(conn)
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

	_, err = client.SetLoggingLevel(ctx, &recoder_grpc.SetLoggingLevelRequest{
		Level: logLevelGo2Protobuf(logLevel),
	})
	if err != nil {
		return fmt.Errorf("query error: %w", err)
	}
	return nil
}

type InputConfig = recoder.InputConfig
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

	resp, err := client.NewInput(ctx, &recoder_grpc.NewInputRequest{
		Path: &recoder_grpc.ResourcePath{
			ResourcePath: &recoder_grpc.ResourcePath_Url{
				Url: &recoder_grpc.ResourcePathURL{
					Url:     url,
					AuthKey: authKey,
				},
			},
		},
		Config: &recoder_grpc.InputConfig{},
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

	_, err = client.CloseInput(ctx, &recoder_grpc.CloseInputRequest{
		InputID: uint64(inputID),
	})
	if err != nil {
		return fmt.Errorf("query error: %w", err)
	}

	return nil
}

type OutputID uint64
type OutputConfig = recoder.OutputConfig

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

	resp, err := client.NewOutput(ctx, &recoder_grpc.NewOutputRequest{
		Path: &recoder_grpc.ResourcePath{
			ResourcePath: &recoder_grpc.ResourcePath_Url{
				Url: &recoder_grpc.ResourcePathURL{
					Url:     url,
					AuthKey: streamKey,
				},
			},
		},
		Config: &recoder_grpc.OutputConfig{},
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

	_, err = client.CloseOutput(ctx, &recoder_grpc.CloseOutputRequest{
		OutputID: uint64(outputID),
	})
	if err != nil {
		return fmt.Errorf("query error: %w", err)
	}

	return nil
}

type RecoderID uint64
type RecoderConfig = recoder.Config

func (c *Client) NewRecoder(
	ctx context.Context,
	config RecoderConfig,
) (RecoderID, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	resp, err := client.NewRecoder(ctx, &recoder_grpc.NewRecoderRequest{
		Config: &recoder_grpc.RecoderConfig{},
	})
	if err != nil {
		return 0, fmt.Errorf("query error: %w", err)
	}

	return RecoderID(resp.GetId()), nil
}

func (c *Client) StartRecoding(
	ctx context.Context,
	recoderID RecoderID,
	inputID InputID,
	outputID OutputID,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.StartRecoding(ctx, &recoder_grpc.StartRecodingRequest{
		RecoderID: uint64(recoderID),
		InputID:   uint64(inputID),
		OutputID:  uint64(outputID),
	})
	if err != nil {
		return fmt.Errorf("query error: %w", err)
	}

	return nil
}

type RecoderStats = recoder.Stats

func (c *Client) GetRecoderStats(
	ctx context.Context,
	recoderID RecoderID,
) (*RecoderStats, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	resp, err := client.GetRecoderStats(ctx, &recoder_grpc.GetRecoderStatsRequest{
		RecoderID: uint64(recoderID),
	})
	if err != nil {
		return nil, fmt.Errorf("query error: %w", err)
	}

	return &RecoderStats{
		BytesCountRead:  resp.GetBytesCountRead(),
		BytesCountWrote: resp.GetBytesCountWrote(),
	}, nil
}

func (c *Client) RecodingEndedChan(
	ctx context.Context,
	recoderID RecoderID,
) (<-chan struct{}, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return nil, err
	}

	waiter, err := client.RecodingEndedChan(ctx, &recoder_grpc.RecodingEndedChanRequest{
		RecoderID: uint64(recoderID),
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
