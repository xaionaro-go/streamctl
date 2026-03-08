package obs

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	streamctl "github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs/types"
)

const ID = types.ID

type Config = types.Config
type StreamProfile = types.StreamProfile
type AccountConfig = types.AccountConfig

func init() {
	streamcontrol.RegisterPlatform[AccountConfig, StreamProfile, struct{}](
		ID,
		0,
		func(
			ctx context.Context,
			cfg AccountConfig,
			saveCfgFn func(AccountConfig) error,
		) (streamcontrol.AccountGeneric[StreamProfile], error) {
			return New(ctx, cfg, saveCfgFn)
		},
	)
}

func InitConfig(cfg streamctl.Config) {
	types.InitConfig(cfg)
}

func GetConfig(
	ctx context.Context,
	cfg streamcontrol.Config,
) *Config {
	return streamcontrol.GetPlatformConfig[AccountConfig, StreamProfile](ctx, cfg, ID)
}
