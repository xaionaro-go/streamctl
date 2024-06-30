package twitch

import (
	streamctl "github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	twitch "github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch/types"
)

const ID = twitch.ID

type Config = twitch.Config
type StreamProfile = twitch.StreamProfile
type PlatformSpecificConfig = twitch.PlatformSpecificConfig
type OAuthHandler = twitch.OAuthHandler

func InitConfig(cfg streamctl.Config) {
	twitch.InitConfig(cfg)
}
