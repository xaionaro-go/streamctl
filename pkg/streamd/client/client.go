package client

import (
	"context"
	"net/url"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
)

type Client struct{}

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
