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
type PlatformSpecificConfig = twitch.PlatformSpecificConfig
type OAuthHandler = twitch.OAuthHandler

func init() {
	streamctl.RegisterPlatform[PlatformSpecificConfig, StreamProfile](ID)
}

func InitConfig(cfg streamctl.Config) {
	twitch.InitConfig(cfg)
}

func GetConfig(
	ctx context.Context,
	cfg streamcontrol.Config,
) *Config {
	return streamcontrol.GetPlatformConfig[PlatformSpecificConfig, StreamProfile](ctx, cfg, ID)
}
