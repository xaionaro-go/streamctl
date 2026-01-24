package youtube

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	streamctl "github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	youtube "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube/types"
)

const ID = youtube.ID
const MaxTitleLength = 100

type OAuthHandler = youtube.OAuthHandler
type Config = youtube.Config
type StreamProfile = youtube.StreamProfile
type AccountConfig = youtube.AccountConfig

func init() {
	streamcontrol.RegisterPlatform[
		AccountConfig,
		StreamProfile,
		StreamStatusCustomData,
	](
		ID,
		MaxTitleLength,
		func(
			ctx context.Context,
			cfg AccountConfig,
			saveCfgFn func(AccountConfig) error,
		) (streamcontrol.AccountGeneric[StreamProfile], error) {
			return New(ctx, cfg, saveCfgFn)
		},
	)
}

func InitConfig(cfg streamctl.Config) {
	youtube.InitConfig(cfg)
}

func GetConfig(
	ctx context.Context,
	cfg streamcontrol.Config,
) *Config {
	return streamcontrol.GetPlatformConfig[AccountConfig, StreamProfile](ctx, cfg, ID)
}

type TemplateTags = youtube.TemplateTags

const (
	UndefinedTemplateTags       = youtube.UndefinedTemplateTags
	TemplateTagsIgnore          = youtube.TemplateTagsIgnore
	TemplateTagsUseAsPrimary    = youtube.TemplateTagsUseAsPrimary
	TemplateTagsUseAsAdditional = youtube.TemplateTagsUseAsAdditional
)
