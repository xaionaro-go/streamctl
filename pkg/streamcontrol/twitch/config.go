package twitch

import (
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

type PlatformSpecificConfig struct {
	Channel         string
	ClientID        string
	ClientSecret    string
	ClientCode      string
	AuthType        string
	AppAccessToken  string
	UserAccessToken string
	RefreshToken    string
}

type Config = streamctl.PlatformConfig[PlatformSpecificConfig, StreamProfile]

func InitConfig(cfg streamctl.Config, id string) {
	streamctl.InitConfig(cfg, id, Config{})
}
