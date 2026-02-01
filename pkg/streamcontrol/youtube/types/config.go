package youtube

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	"github.com/xaionaro-go/streamctl/pkg/secret"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	streamctl "github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"golang.org/x/oauth2"
)

const ID = streamctl.PlatformID("youtube")

type OAuthHandler func(context.Context, oauthhandler.OAuthHandlerArgument) error

type OAuth2Token = secret.Any[oauth2.Token]

type AccountConfig struct {
	streamcontrol.AccountConfigBase[StreamProfile] `yaml:",inline"`

	AllowlistedStreamIDs []streamcontrol.StreamID `yaml:"allowlisted_stream_ids,omitempty"`
	ChannelID            string
	ClientID             string
	ClientSecret         secret.String
	Token                *OAuth2Token
	CustomOAuthHandler   OAuthHandler    `yaml:"-"`
	GetOAuthListenPorts  func() []uint16 `yaml:"-"`
}

type Config = streamctl.PlatformConfig[AccountConfig, StreamProfile]

func InitConfig(cfg streamctl.Config) {
	streamctl.InitConfig(cfg, ID, Config{})
}

func (cfg AccountConfig) IsInitialized() bool {
	return cfg.ClientID != "" && cfg.ClientSecret.Get() != ""
}

type TemplateTags string

const (
	UndefinedTemplateTags       = TemplateTags("")
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
		UndefinedTemplateTags,
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
	streamctl.StreamProfileBase `yaml:",inline"`

	AutoNumerate         bool
	TemplateBroadcastIDs []string
	Tags                 []string
	TemplateTags         TemplateTags
}
