package obs

import (
	streamctl "github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs/types"
)

const ID = types.ID

type Config = types.Config
type StreamProfile = types.StreamProfile
type PlatformSpecificConfig = types.PlatformSpecificConfig

func init() {
	streamctl.RegisterPlatform[PlatformSpecificConfig, StreamProfile](ID)
}

func InitConfig(cfg streamctl.Config) {
	types.InitConfig(cfg)
}
