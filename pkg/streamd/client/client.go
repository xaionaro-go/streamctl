package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	obs "github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs/types"
	twitch "github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch/types"
	youtube "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube/types"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"google.golang.org/grpc"
)

type Client struct {
	URL *url.URL
}

var _ api.StreamD = (*Client)(nil)

func New(url *url.URL) *Client {
	return &Client{URL: url}
}

func (c *Client) grpcClient() (streamd_grpc.StreamDClient, *grpc.ClientConn, error) {
	conn, err := grpc.NewClient(c.URL.String())
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
	b, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("unable to serialize the config: %w", err)
	}

	req := &streamd_grpc.SetConfigRequest{
		Config: string(b),
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

	b, err := json.Marshal(profile)
	if err != nil {
		return fmt.Errorf("unable to serialize the profile: %w", err)
	}

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
		data = &api.BackendDataOBS{}
	case twitch.ID:
		data = &api.BackendDataTwitch{}
	case youtube.ID:
		data = &api.BackendDataYouTube{}
	default:
		return nil, fmt.Errorf("unknown platform: '%s'", platID)
	}

	err = json.Unmarshal([]byte(reply.GetData()), data)
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
