package youtube

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	streamctl "github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	youtube "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube/types"
)

const ID = youtube.ID

type OAuthHandler = youtube.OAuthHandler
type Config = youtube.Config
type StreamProfile = youtube.StreamProfile
type PlatformSpecificConfig = youtube.PlatformSpecificConfig

func init() {
	streamctl.RegisterPlatform[PlatformSpecificConfig, StreamProfile](ID)
}

func InitConfig(cfg streamctl.Config) {
	youtube.InitConfig(cfg)
}

func GetConfig(
	ctx context.Context,
	cfg streamcontrol.Config,
) *Config {
	return streamcontrol.GetPlatformConfig[PlatformSpecificConfig, StreamProfile](ctx, cfg, ID)
}

type TemplateTags = youtube.TemplateTags

const (
	TemplateTagsUndefined       = youtube.TemplateTagsUndefined
	TemplateTagsIgnore          = youtube.TemplateTagsIgnore
	TemplateTagsUseAsPrimary    = youtube.TemplateTagsUseAsPrimary
	TemplateTagsUseAsAdditional = youtube.TemplateTagsUseAsAdditional
)
