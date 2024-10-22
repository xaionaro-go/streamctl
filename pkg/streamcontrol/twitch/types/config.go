package twitch

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	streamctl "github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

const ID = streamctl.PlatformName("twitch")

type OAuthHandler func(context.Context, oauthhandler.OAuthHandlerArgument) error

type PlatformSpecificConfig struct {
	Channel             string
	ClientID            string `secret:"true"`
	ClientSecret        string `secret:"true"`
	ClientCode          string `secret:"true"`
	AuthType            string
	AppAccessToken      string          `secret:"true"`
	UserAccessToken     string          `secret:"true"`
	RefreshToken        string          `secret:"true"`
	CustomOAuthHandler  OAuthHandler    `yaml:"-"`
	GetOAuthListenPorts func() []uint16 `yaml:"-"`
}

type Config = streamctl.PlatformConfig[PlatformSpecificConfig, StreamProfile]

func InitConfig(cfg streamctl.Config) {
	streamctl.InitConfig(cfg, ID, Config{})
}

func (cfg PlatformSpecificConfig) IsInitialized() bool {
	return cfg.Channel != "" && cfg.ClientID != ""
}

type StreamProfile struct {
	streamctl.StreamProfileBase `yaml:",omitempty,inline,alias"`

	Tags         [10]string
	Language     *string
	CategoryName *string
	CategoryID   *string
}
