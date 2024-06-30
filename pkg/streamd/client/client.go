package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	obs "github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs/types"
	twitch "github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch/types"
	youtube "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube/types"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	Target string
}

var _ api.StreamD = (*Client)(nil)

func New(target string) *Client {
	return &Client{Target: target}
}

func (c *Client) grpcClient() (streamd_grpc.StreamDClient, *grpc.ClientConn, error) {
	conn, err := grpc.NewClient(
		c.Target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to initialize a gRPC client: %w", err)
	}

	client := streamd_grpc.NewStreamDClient(conn)
	return client, conn, nil
}

func (c *Client) Run(ctx context.Context) error {
	return nil
}

func (c *Client) FetchConfig(ctx context.Context) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.OBSOLETE_FetchConfig(ctx, &streamd_grpc.OBSOLETE_FetchConfigRequest{})
	return err
}

func (c *Client) InitCache(ctx context.Context) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.InitCache(ctx, &streamd_grpc.InitCacheRequest{})
	return err
}

func (c *Client) SaveConfig(ctx context.Context) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.SaveConfig(ctx, &streamd_grpc.SaveConfigRequest{})
	return err
}

func (c *Client) ResetCache(ctx context.Context) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.ResetCache(ctx, &streamd_grpc.ResetCacheRequest{})
	return err
}

func (c *Client) GetConfig(ctx context.Context) (*config.Config, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	reply, err := client.GetConfig(ctx, &streamd_grpc.GetConfigRequest{})
	if err != nil {
		return nil, fmt.Errorf("unable to request the config: %w", err)
	}

	var result config.Config
	err = config.ReadConfig(ctx, []byte(reply.Config), &result)
	if err != nil {
		return nil, fmt.Errorf("unable to unserialize the received config: %w", err)
	}
	return &result, nil
}

func (c *Client) SetConfig(ctx context.Context, cfg *config.Config) error {
	var buf bytes.Buffer
	err := config.WriteConfig(ctx, &buf, *cfg)
	if err != nil {
		return fmt.Errorf("unable to serialize the config: %w", err)
	}

	req := &streamd_grpc.SetConfigRequest{
		Config: buf.String(),
	}

	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.SetConfig(ctx, req)
	return err
}

func (c *Client) IsBackendEnabled(ctx context.Context, id streamcontrol.PlatformName) (bool, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return false, err
	}
	defer conn.Close()

	reply, err := client.GetBackendInfo(ctx, &streamd_grpc.GetBackendInfoRequest{
		PlatID: string(id),
	})
	if err != nil {
		return false, fmt.Errorf("unable to get backend info: %w", err)
	}
	return reply.IsInitialized, nil
}

func (c *Client) OBSOLETE_IsGITInitialized(ctx context.Context) (bool, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return false, err
	}
	defer conn.Close()

	reply, err := client.OBSOLETE_GitInfo(ctx, &streamd_grpc.OBSOLETE_GetGitInfoRequest{})
	if err != nil {
		return false, fmt.Errorf("unable to get backend info: %w", err)
	}
	return reply.IsInitialized, nil
}

func (c *Client) StartStream(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	title string, description string,
	profile streamcontrol.AbstractStreamProfile,
	customArgs ...any,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	b, err := yaml.Marshal(profile)
	if err != nil {
		return fmt.Errorf("unable to serialize the profile: %w", err)
	}

	logger.Debugf(ctx, "serialized profile: '%s'", profile)

	_, err = client.StartStream(ctx, &streamd_grpc.StartStreamRequest{
		PlatID:      string(platID),
		Title:       title,
		Description: description,
		Profile:     string(b),
	})
	if err != nil {
		return fmt.Errorf("unable to start the stream: %w", err)
	}

	return nil
}
func (c *Client) EndStream(ctx context.Context, platID streamcontrol.PlatformName) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.EndStream(ctx, &streamd_grpc.EndStreamRequest{PlatID: string(platID)})
	if err != nil {
		return fmt.Errorf("unable to end the stream: %w", err)
	}

	return nil
}

func (c *Client) OBSOLETE_GitRelogin(ctx context.Context) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.OBSOLETE_GitRelogin(ctx, &streamd_grpc.OBSOLETE_GitReloginRequest{})
	if err != nil {
		return fmt.Errorf("unable force git relogin: %w", err)
	}

	return nil
}

func (c *Client) GetBackendData(ctx context.Context, platID streamcontrol.PlatformName) (any, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	reply, err := client.GetBackendInfo(ctx, &streamd_grpc.GetBackendInfoRequest{
		PlatID: string(platID),
	})
	if err != nil {
		return nil, fmt.Errorf("unable to get backend info: %w", err)
	}

	var data any
	switch platID {
	case obs.ID:
		_data := api.BackendDataOBS{}
		err = json.Unmarshal([]byte(reply.GetData()), &_data)
		data = _data
	case twitch.ID:
		_data := api.BackendDataTwitch{}
		err = json.Unmarshal([]byte(reply.GetData()), &_data)
		data = _data
	case youtube.ID:
		_data := api.BackendDataYouTube{}
		err = json.Unmarshal([]byte(reply.GetData()), &_data)
		data = _data
	default:
		return nil, fmt.Errorf("unknown platform: '%s'", platID)
	}

	if err != nil {
		return nil, fmt.Errorf("unable to deserialize data: %w", err)
	}
	return data, nil
}

func (c *Client) Restart(ctx context.Context) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.Restart(ctx, &streamd_grpc.RestartRequest{})
	if err != nil {
		return fmt.Errorf("unable restart the server: %w", err)
	}

	return nil
}

func (c *Client) EXPERIMENTAL_ReinitStreamControllers(ctx context.Context) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.EXPERIMENTAL_ReinitStreamControllers(ctx, &streamd_grpc.EXPERIMENTAL_ReinitStreamControllersRequest{})
	if err != nil {
		return fmt.Errorf("unable restart the server: %w", err)
	}

	return nil

}

func (c *Client) GetStreamStatus(
	ctx context.Context,
	platID streamcontrol.PlatformName,
) (*streamcontrol.StreamStatus, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	streamStatus, err := client.GetStreamStatus(ctx, &streamd_grpc.GetStreamStatusRequest{
		PlatID: string(platID),
	})
	if err != nil {
		return nil, fmt.Errorf("unable to get the stream status of '%s': %w", platID, err)
	}

	var startedAt *time.Time
	if streamStatus != nil && streamStatus.StartedAt != nil {
		v := *streamStatus.StartedAt
		startedAt = ptr(time.Unix(v/1000000000, v%1000000000))
	}

	return &streamcontrol.StreamStatus{
		IsActive:  streamStatus.GetIsActive(),
		StartedAt: startedAt,
	}, nil
}

func (c *Client) SetTitle(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	title string,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.SetTitle(ctx, &streamd_grpc.SetTitleRequest{
		PlatID: string(platID),
		Title:  title,
	})
	return err
}
func (c *Client) SetDescription(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	description string,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.SetDescription(ctx, &streamd_grpc.SetDescriptionRequest{
		PlatID:      string(platID),
		Description: description,
	})
	return err
}
func (c *Client) ApplyProfile(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	profile streamcontrol.AbstractStreamProfile,
	customArgs ...any,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	b, err := yaml.Marshal(profile)
	if err != nil {
		return fmt.Errorf("unable to serialize the profile: %w", err)
	}

	logger.Debugf(ctx, "serialized profile: '%s'", profile)

	_, err = client.SetApplyProfile(ctx, &streamd_grpc.SetApplyProfileRequest{
		PlatID:  string(platID),
		Profile: string(b),
	})
	if err != nil {
		return fmt.Errorf("unable to apply the profile to the stream: %w", err)
	}

	return nil
}

func (c *Client) UpdateStream(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	title string, description string,
	profile streamcontrol.AbstractStreamProfile,
	customArgs ...any,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	b, err := yaml.Marshal(profile)
	if err != nil {
		return fmt.Errorf("unable to serialize the profile: %w", err)
	}

	logger.Debugf(ctx, "serialized profile: '%s'", profile)

	_, err = client.UpdateStream(ctx, &streamd_grpc.UpdateStreamRequest{
		PlatID:      string(platID),
		Title:       title,
		Description: description,
		Profile:     string(b),
	})
	if err != nil {
		return fmt.Errorf("unable to update the stream: %w", err)
	}

	return nil
}

func ptr[T any](in T) *T {
	return &in
}
