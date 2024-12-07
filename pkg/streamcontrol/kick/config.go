package kick

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	streamctl "github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	kick "github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick/types"
)

const ID = kick.ID

type Config = kick.Config
type StreamProfile = kick.StreamProfile
type PlatformSpecificConfig = kick.PlatformSpecificConfig
type OAuthHandler = kick.OAuthHandler

func init() {
	streamctl.RegisterPlatform[PlatformSpecificConfig, StreamProfile](ID)
}

func InitConfig(cfg streamctl.Config) {
	kick.InitConfig(cfg)
}

func GetConfig(
	ctx context.Context,
	cfg streamcontrol.Config,
) *Config {
	return streamcontrol.GetPlatformConfig[PlatformSpecificConfig, StreamProfile](ctx, cfg, ID)
}
