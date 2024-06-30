package obs

import (
	streamctl "github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

const ID = streamctl.PlatformName("obs")

type PlatformSpecificConfig struct {
	Host     string
	Port     uint16
	Password string `yaml:"pass" json:"pass"`
}

type Config = streamctl.PlatformConfig[PlatformSpecificConfig, StreamProfile]

func InitConfig(cfg streamctl.Config) {
	streamctl.InitConfig(cfg, ID, Config{})
}

type StreamProfile struct {
	streamctl.StreamProfileBase `yaml:",omitempty,inline,alias"`

	EnableRecording bool `yaml:"enable_recording" json:"enable_recording"`
}
