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
type PlatformSpecificConfig = kick.PlatformSpecificConfig
type OAuthHandler = kick.OAuthHandler

func init() {
	streamcontrol.RegisterPlatform[PlatformSpecificConfig, StreamProfile](ID)
}

func InitConfig(cfg streamcontrol.Config) {
	kick.InitConfig(cfg)
}

func GetConfig(
	ctx context.Context,
	cfg streamcontrol.Config,
) *Config {
	return streamcontrol.GetPlatformConfig[PlatformSpecificConfig, StreamProfile](ctx, cfg, ID)
}
