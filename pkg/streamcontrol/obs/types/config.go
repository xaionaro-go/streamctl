package types

import (
	streamctl "github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

const ID = streamctl.PlatformName("obs")

type SceneName string

type PlatformSpecificConfig struct {
	Host     string
	Port     uint16
	Password string `yaml:"pass" json:"pass"`

	SceneRulesByScene map[SceneName]SceneRules `yaml:"scene_rules" json:"scene_rules"`
}

type Config = streamctl.PlatformConfig[PlatformSpecificConfig, StreamProfile]

func InitConfig(cfg streamctl.Config) {
	streamctl.InitConfig(cfg, ID, Config{})
}

type StreamProfile struct {
	streamctl.StreamProfileBase `yaml:",omitempty,inline,alias"`

	EnableRecording bool `yaml:"enable_recording" json:"enable_recording"`
}
