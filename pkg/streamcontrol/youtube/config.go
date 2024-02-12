package youtube

import (
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"golang.org/x/oauth2"
)

type PlatformSpecificConfig struct {
	ClientID     string
	ClientSecret string
	Token        *oauth2.Token
}

type Config = streamctl.PlatformConfig[PlatformSpecificConfig, StreamProfile]

func InitConfig(cfg streamctl.Config, id string) {
	streamctl.InitConfig(cfg, id, Config{})
}
