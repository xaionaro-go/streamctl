package kick

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	streamctl "github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

const ID = streamctl.PlatformName("kick")

type OAuthHandler func(context.Context, oauthhandler.OAuthHandlerArgument) error

type PlatformSpecificConfig struct {
	Channel string
}

type Config = streamctl.PlatformConfig[PlatformSpecificConfig, StreamProfile]

func InitConfig(cfg streamctl.Config) {
	streamctl.InitConfig(cfg, ID, Config{})
}

type StreamProfile struct {
	streamctl.StreamProfileBase `yaml:",omitempty,inline,alias"`
}
