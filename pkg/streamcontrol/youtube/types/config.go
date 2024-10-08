package youtube

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	streamctl "github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"golang.org/x/oauth2"
)

const ID = streamctl.PlatformName("youtube")

type OAuthHandler func(context.Context, oauthhandler.OAuthHandlerArgument) error

type PlatformSpecificConfig struct {
	ClientID            string
	ClientSecret        string
	Token               *oauth2.Token
	CustomOAuthHandler  OAuthHandler    `yaml:"-"`
	GetOAuthListenPorts func() []uint16 `yaml:"-"`
}

type Config = streamctl.PlatformConfig[PlatformSpecificConfig, StreamProfile]

func InitConfig(cfg streamctl.Config) {
	streamctl.InitConfig(cfg, ID, Config{})
}

type TemplateTags string

const (
	TemplateTagsUndefined       = TemplateTags("")
	TemplateTagsIgnore          = TemplateTags("ignore")
	TemplateTagsUseAsPrimary    = TemplateTags("use_as_primary")
	TemplateTagsUseAsAdditional = TemplateTags("use_as_additional")
)

func (t TemplateTags) String() string {
	return string(t)
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
