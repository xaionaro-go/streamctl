package youtube

import (
	streamctl "github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"golang.org/x/oauth2"
)

const ID = streamctl.PlatformName("youtube")

type PlatformSpecificConfig struct {
	ClientID     string
	ClientSecret string
	Token        *oauth2.Token
}

type Config = streamctl.PlatformConfig[PlatformSpecificConfig, StreamProfile]

func InitConfig(cfg streamctl.Config) {
	streamctl.InitConfig(cfg, ID, Config{})
}

type StreamProfile struct {
	streamctl.StreamProfileBase `yaml:",omitempty,inline,alias"`

	Tags []string
}
