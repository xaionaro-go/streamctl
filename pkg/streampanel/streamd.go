package streampanel

import (
	"context"
	"net/url"

	"github.com/facebookincubator/go-belt"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd"
	"github.com/xaionaro-go/streamctl/pkg/streamd/client"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamd/ui"
)

type StreamD interface {
	FetchConfig(ctx context.Context) error
	ResetCache(ctx context.Context) error
	InitCache(ctx context.Context) error
	SetPlatformConfig(
		ctx context.Context,
		platID streamcontrol.PlatformName,
		platCfg *streamcontrol.AbstractPlatformConfig,
	) error
	SaveConfig(ctx context.Context) error
	GetConfig(ctx context.Context) (*config.Config, error)
	SetConfig(ctx context.Context, cfg *config.Config) error
}

var _ StreamD = (*streamd.StreamD)(nil)
var _ StreamD = (*client.Client)(nil)

func NewBuiltinStreamD(configPath string, ui ui.UI, b *belt.Belt) (*streamd.StreamD, error) {
	return streamd.New(configPath, ui, b)
}

func NewRemoteStreamD(url *url.URL, ui ui.UI, _ *belt.Belt) *client.Client {
	return client.New(url)
}
