package types

import (
	"github.com/xaionaro-go/streamctl/pkg/secret"
	streamctl "github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

const ID = streamctl.PlatformName("obs")

type PlatformSpecificConfig struct {
	Host     string
	Port     uint16
	Password secret.String `yaml:"pass" json:"pass"`
}

type Config = streamctl.PlatformConfig[PlatformSpecificConfig, StreamProfile]

func InitConfig(cfg streamctl.Config) {
	streamctl.InitConfig(cfg, ID, Config{})
}

func (cfg PlatformSpecificConfig) IsInitialized() bool {
	return cfg.Host != "" && cfg.Port != 0
}

type StreamProfile struct {
	streamctl.StreamProfileBase `yaml:",omitempty,inline,alias"`

	EnableRecording bool `yaml:"enable_recording" json:"enable_recording"`
}
