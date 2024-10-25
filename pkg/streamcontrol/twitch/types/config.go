package twitch

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/buildvars"
	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	"github.com/xaionaro-go/streamctl/pkg/secret"
	streamctl "github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

const ID = streamctl.PlatformName("twitch")

type OAuthHandler func(context.Context, oauthhandler.OAuthHandlerArgument) error

type PlatformSpecificConfig struct {
	Channel             string
	ClientID            string
	ClientSecret        secret.String
	ClientCode          secret.String
	AuthType            string
	AppAccessToken      secret.String
	UserAccessToken     secret.String
	RefreshToken        secret.String
	CustomOAuthHandler  OAuthHandler    `yaml:"-"`
	GetOAuthListenPorts func() []uint16 `yaml:"-"`
}

type Config = streamctl.PlatformConfig[PlatformSpecificConfig, StreamProfile]

func InitConfig(cfg streamctl.Config) {
	streamctl.InitConfig(cfg, ID, Config{})
}

func (cfg PlatformSpecificConfig) IsInitialized() bool {
	return cfg.Channel != "" &&
		valueOrDefault(cfg.ClientID, buildvars.TwitchClientID) != "" &&
		valueOrDefault(cfg.ClientSecret.Get(), buildvars.TwitchClientSecret) != ""
}

type StreamProfile struct {
	streamctl.StreamProfileBase `yaml:",omitempty,inline,alias"`

	Tags         [10]string
	Language     *string
	CategoryName *string
	CategoryID   *string
}
