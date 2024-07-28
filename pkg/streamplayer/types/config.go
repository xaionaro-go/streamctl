package types

import (
	"context"
	"time"
)

type Config struct {
	JitterBufDuration     time.Duration
	CatchupMaxSpeedFactor float64
	MaxCatchupAtLag       time.Duration
	StartTimeout          time.Duration
	ReadTimeout           time.Duration
}

func (cfg Config) Options() Options {
	return Options{
		OptionJitterBufDuration(cfg.JitterBufDuration),
		OptionCatchupMaxSpeedFactor(cfg.CatchupMaxSpeedFactor),
		OptionMaxCatchupAtLag(cfg.MaxCatchupAtLag),
		OptionStartTimeout(cfg.StartTimeout),
		OptionReadTimeout(cfg.ReadTimeout),
	}
}

type Option interface {
	Apply(cfg *Config)
}

type Options []Option

func (s Options) Config() Config {
	cfg := DefaultConfig(context.Background())
	s.apply(&cfg)
	return cfg
}

func (s Options) apply(cfg *Config) {
	for _, opt := range s {
		opt.Apply(cfg)
	}
}

var DefaultConfig = func(ctx context.Context) Config {
	return Config{
		JitterBufDuration:     3 * time.Second,
		CatchupMaxSpeedFactor: 10,
		MaxCatchupAtLag:       21 * time.Second,
		StartTimeout:          60 * time.Second,
		ReadTimeout:           10 * time.Second,
	}
}

type OptionJitterBufDuration time.Duration

func (s OptionJitterBufDuration) Apply(cfg *Config) {
	cfg.JitterBufDuration = time.Duration(s)
}

type OptionCatchupMaxSpeedFactor float64

func (s OptionCatchupMaxSpeedFactor) Apply(cfg *Config) {
	cfg.CatchupMaxSpeedFactor = float64(s)
}

type OptionMaxCatchupAtLag time.Duration

func (s OptionMaxCatchupAtLag) Apply(cfg *Config) {
	cfg.MaxCatchupAtLag = time.Duration(s)
}

type OptionStartTimeout time.Duration

func (s OptionStartTimeout) Apply(cfg *Config) {
	cfg.StartTimeout = time.Duration(s)
}

type OptionReadTimeout time.Duration

func (s OptionReadTimeout) Apply(cfg *Config) {
	cfg.ReadTimeout = time.Duration(s)
}
