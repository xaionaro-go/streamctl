package types

import (
	player "github.com/xaionaro-go/player/pkg/player/types"
	sptypes "github.com/xaionaro-go/streamctl/pkg/streamplayer/types"
)

type StreamPlayer struct {
	StreamID             StreamID
	PlayerType           player.Backend
	Disabled             bool
	StreamPlaybackConfig sptypes.Config
}

type StreamPlayerConfig struct {
	DefaultStreamPlayerOptions sptypes.Options
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

type StreamPlayerOptionDefaultOptions sptypes.Options

func (opt StreamPlayerOptionDefaultOptions) apply(cfg *StreamPlayerConfig) {
	cfg.DefaultStreamPlayerOptions = (sptypes.Options)(opt)
}
