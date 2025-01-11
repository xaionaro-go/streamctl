package types

import (
	player "github.com/xaionaro-go/player/pkg/player/types"
	"github.com/xaionaro-go/streamctl/pkg/streamplayer"
	sptypes "github.com/xaionaro-go/streamctl/pkg/streamplayer/types"
)

type StreamPlayer struct {
	StreamID             StreamID
	PlayerType           player.Backend
	Disabled             bool
	StreamPlaybackConfig sptypes.Config
}

type StreamPlayerConfig struct {
	DefaultStreamPlayerOptions streamplayer.Options
}

type StreamPlayerOption interface {
	apply(*StreamPlayerConfig)
}

type StreamPlayerOptions []StreamPlayerOption

func (s StreamPlayerOptions) Config() StreamPlayerConfig {
	cfg := StreamPlayerConfig{}
	for _, opt := range s {
		opt.apply(&cfg)
	}
	return cfg
}

type StreamPlayerOptionDefaultOptions streamplayer.Options

func (opt StreamPlayerOptionDefaultOptions) apply(cfg *StreamPlayerConfig) {
	cfg.DefaultStreamPlayerOptions = (streamplayer.Options)(opt)
}
