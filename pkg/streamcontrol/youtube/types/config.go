package youtube

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	"github.com/xaionaro-go/streamctl/pkg/secret"
	streamctl "github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"golang.org/x/oauth2"
)

const ID = streamctl.PlatformName("youtube")

type OAuthHandler func(context.Context, oauthhandler.OAuthHandlerArgument) error

type OAuth2Token = secret.Any[oauth2.Token]

type PlatformSpecificConfig struct {
	ChannelID           string
	ClientID            string
	ClientSecret        secret.String
	Token               *OAuth2Token
	CustomOAuthHandler  OAuthHandler    `yaml:"-"`
	GetOAuthListenPorts func() []uint16 `yaml:"-"`

	// YTProxyAddr is the address of a youtubeapiproxy gRPC server.
	// When set, chat listeners use it for broadcast discovery and streaming.
	YTProxyAddr string `yaml:"yt_proxy_addr"`

	// APIKey is a YouTube Data API v3 key for unauthenticated polling.
	// Prefer OAuth2 credentials (ClientID+ClientSecret+Token) when available.
	APIKey string `yaml:"api_key"`
}

type Config = streamctl.PlatformConfig[PlatformSpecificConfig, StreamProfile]

func InitConfig(cfg streamctl.Config) {
	streamctl.InitConfig(cfg, ID, Config{})
}

func (cfg PlatformSpecificConfig) IsInitialized() bool {
	return cfg.ClientID != "" && cfg.ClientSecret.Get() != ""
}

type TemplateTags string

const (
	TemplateTagsUndefined       = TemplateTags("")
	TemplateTagsIgnore          = TemplateTags("ignore")
	TemplateTagsUseAsPrimary    = TemplateTags("use_as_primary")
	TemplateTagsUseAsAdditional = TemplateTags("use_as_additional")
)

func (t *TemplateTags) String() string {
	if t == nil {
		return "null"
	}
	return string(*t)
}

func (t *TemplateTags) Parse(in string) error {
	for _, candidate := range []TemplateTags{
		TemplateTagsUndefined,
		TemplateTagsIgnore,
		TemplateTagsUseAsPrimary,
		TemplateTagsUseAsAdditional,
	} {
		if TemplateTags(in) == candidate {
			*t = candidate
			return nil
		}
	}
	return fmt.Errorf("unknown/unexpected value: '%s'", in)
}

type StreamProfile struct {
	streamctl.StreamProfileBase `yaml:",omitempty,inline,alias"`

	AutoNumerate         bool
	TemplateBroadcastIDs []string
	Tags                 []string
	TemplateTags         TemplateTags
}
