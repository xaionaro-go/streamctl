package kick

import (
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
