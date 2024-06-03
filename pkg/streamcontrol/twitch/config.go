package twitch

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	streamctl "github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

const ID = streamctl.PlatformName("twitch")

type OAuthHandler func(context.Context, oauthhandler.OAuthHandlerArgument) error

type PlatformSpecificConfig struct {
	Channel            string
	ClientID           string
	ClientSecret       string
	ClientCode         string
	AuthType           string
	AppAccessToken     string
	UserAccessToken    string
	RefreshToken       string
	CustomOAuthHandler OAuthHandler `yaml:"-"`
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
