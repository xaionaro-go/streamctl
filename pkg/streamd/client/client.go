package client

import (
	"context"
	"net/url"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
)

type Client struct{}

var _ api.StreamD = (*Client)(nil)

func New(url *url.URL) *Client {
	panic("not implemented")
}

func (c *Client) FetchConfig(ctx context.Context) error {
	panic("not implemented")
}

func (c *Client) InitCache(ctx context.Context) error {
	panic("not implemented")
}

func (c *Client) SetPlatformConfig(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	platCfg *streamcontrol.AbstractPlatformConfig,
) error {
	panic("not implemented")
}

func (c *Client) SaveConfig(ctx context.Context) error {
	panic("not implemented")
}

func (c *Client) ResetCache(ctx context.Context) error {
	panic("not implemented")
}

func (c *Client) GetConfig(ctx context.Context) (*config.Config, error) {
	panic("not implemented")
}

func (c *Client) SetConfig(ctx context.Context, cfg *config.Config) error {
	panic("not implemented")
}

func (c *Client) IsBackendEnabled(ctx context.Context, id streamcontrol.PlatformName) (bool, error) {
	panic("not implemented")
}

func (c *Client) IsGITInitialized(ctx context.Context) (bool, error) {
	panic("not implemented")
}

func (c *Client) StartStream(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	title string, description string,
	profile streamcontrol.AbstractStreamProfile,
	customArgs ...any,
) error {
	panic("not implemented")
}
func (c *Client) EndStream(ctx context.Context, platID streamcontrol.PlatformName) error {
	panic("not implemented")
}

func (c *Client) GitRelogin(ctx context.Context) error {
	panic("not implemented")
}

func (c *Client) GetBackendData(ctx context.Context, platID streamcontrol.PlatformName) (any, error) {
	panic("not implemented")
}
