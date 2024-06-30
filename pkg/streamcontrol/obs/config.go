package obs

import (
	streamctl "github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	obs "github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs/types"
)

const ID = obs.ID

type Config = obs.Config
type StreamProfile = obs.StreamProfile
type PlatformSpecificConfig = obs.PlatformSpecificConfig

func InitConfig(cfg streamctl.Config) {
	obs.InitConfig(cfg)
}
