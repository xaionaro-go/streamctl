package twitch

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	streamctl "github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	twitch "github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch/types"
)

const ID = twitch.ID

type Config = twitch.Config
type StreamProfile = twitch.StreamProfile
type AccountConfig = twitch.AccountConfig
type OAuthHandler = twitch.OAuthHandler

func init() {
	streamctl.RegisterPlatform[AccountConfig, StreamProfile, struct{}](
		ID,
		twitch.MaxTitleLength,
		func(
			ctx context.Context,
			cfg AccountConfig,
			saveCfgFn func(AccountConfig) error,
		) (streamctl.AccountGeneric[StreamProfile], error) {
			return New(ctx, cfg, saveCfgFn)
		},
	)
}

func InitConfig(cfg streamctl.Config) {
	twitch.InitConfig(cfg)
}

func GetConfig(
	ctx context.Context,
	cfg streamcontrol.Config,
) *Config {
	return streamcontrol.GetPlatformConfig[AccountConfig, StreamProfile](ctx, cfg, ID)
}
