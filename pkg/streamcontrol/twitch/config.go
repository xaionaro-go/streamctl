package twitch

import (
	streamctl "github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

const ID = streamctl.PlatformName("twitch")

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

func InitConfig(cfg streamctl.Config) {
	streamctl.InitConfig(cfg, ID, Config{})
}

type StreamProfile struct {
	streamctl.StreamProfileBase `yaml:",omitempty,inline,alias"`

	Tags         []string
	Language     *string
	CategoryName *string
	CategoryID   *string
}
