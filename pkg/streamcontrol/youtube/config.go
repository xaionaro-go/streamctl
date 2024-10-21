package youtube

import (
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

type TemplateTags = youtube.TemplateTags

const (
	TemplateTagsUndefined       = youtube.TemplateTagsUndefined
	TemplateTagsIgnore          = youtube.TemplateTagsIgnore
	TemplateTagsUseAsPrimary    = youtube.TemplateTagsUseAsPrimary
	TemplateTagsUseAsAdditional = youtube.TemplateTagsUseAsAdditional
)
