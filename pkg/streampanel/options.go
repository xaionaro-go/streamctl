package streampanel

import (
	"github.com/xaionaro-go/streamctl/pkg/streampanel/config"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/consts"
)

type Config = config.Config

type Option interface {
	Apply(cfg *Config)
}

type Options []Option

func (options Options) ApplyOverrides(cfg Config) Config {
	for _, option := range options {
		option.Apply(&cfg)
	}
	return cfg
}

type OptionRemoteStreamDAddr string

func (o OptionRemoteStreamDAddr) Apply(cfg *Config) {
	cfg.RemoteStreamDAddr = string(o)
}

type OptionOAuthListenPortTwitch uint16

func (o OptionOAuthListenPortTwitch) Apply(cfg *Config) {
	cfg.OAuth.ListenPorts.Twitch = uint16(o)
}

type OptionOAuthListenPortYouTube uint16

func (o OptionOAuthListenPortYouTube) Apply(cfg *Config) {
	cfg.OAuth.ListenPorts.YouTube = uint16(o)
}

type loopConfig struct {
	StartingPage consts.Page
	AutoUpdater  AutoUpdater `yaml:"-"` // TODO: remove this from here
}

type LoopOption interface {
	apply(*loopConfig)
}

type loopOptions []LoopOption

func (s loopOptions) Config() loopConfig {
	cfg := loopConfig{
		StartingPage: consts.PageControl,
	}
	for _, opt := range s {
		opt.apply(&cfg)
	}
	return cfg
}

type LoopOptionStartingPage string

func (opt LoopOptionStartingPage) apply(cfg *loopConfig) {
	cfg.StartingPage = consts.Page(opt)
}

type LoopOptionAutoUpdater struct{ AutoUpdater }

func (o LoopOptionAutoUpdater) apply(cfg *loopConfig) {
	cfg.AutoUpdater = o.AutoUpdater
}
