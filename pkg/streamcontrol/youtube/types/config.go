package youtube

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	streamctl "github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"golang.org/x/oauth2"
)

const ID = streamctl.PlatformName("youtube")

type OAuthHandler func(context.Context, oauthhandler.OAuthHandlerArgument) error

type PlatformSpecificConfig struct {
	ClientID           string
	ClientSecret       string
	Token              *oauth2.Token
	CustomOAuthHandler OAuthHandler `yaml:"-"`
}

type Config = streamctl.PlatformConfig[PlatformSpecificConfig, StreamProfile]

func InitConfig(cfg streamctl.Config) {
	streamctl.InitConfig(cfg, ID, Config{})
}

type StreamProfile struct {
	streamctl.StreamProfileBase `yaml:",omitempty,inline,alias"`

	AutoNumerate         bool
	TemplateBroadcastIDs []string
	Tags                 []string
}
