package kick

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	kick "github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick/types"
)

const ID = kick.ID

type Config = kick.Config
type CustomData = kick.CustomData
type StreamProfile = kick.StreamProfile
type AccountConfig = kick.AccountConfig
type OAuthHandler = kick.OAuthHandler

func init() {
	streamcontrol.RegisterPlatform[
		AccountConfig,
		StreamProfile,
		struct{},
	](
		ID,
		kick.MaxTitleLength,
		func(
			ctx context.Context,
			cfg AccountConfig,
			saveCfgFn func(AccountConfig) error,
		) (streamcontrol.AccountGeneric[StreamProfile], error) {
			return New(ctx, cfg, saveCfgFn)
		},
	)
}

func InitConfig(cfg streamcontrol.Config) {
	kick.InitConfig(cfg)
}

func GetConfig(
	ctx context.Context,
	cfg streamcontrol.Config,
) *Config {
	return streamcontrol.GetPlatformConfig[AccountConfig, StreamProfile](ctx, cfg, ID)
}
