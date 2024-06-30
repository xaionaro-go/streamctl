package youtube

import (
	streamctl "github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	youtube "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube/types"
)

const ID = youtube.ID

type OAuthHandler = youtube.OAuthHandler
type Config = youtube.Config
type StreamProfile = youtube.StreamProfile
type PlatformSpecificConfig = youtube.PlatformSpecificConfig

func InitConfig(cfg streamctl.Config) {
	youtube.InitConfig(cfg)
}
