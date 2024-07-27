package types

import (
	"context"
	"time"
)

type Config struct {
	JitterBufDuration     time.Duration
	CatchupMaxSpeedFactor float64
	MaxCatchupAtLag       time.Duration
	ReadTimeout           time.Duration
}

type Option interface {
	apply(cfg *Config)
}

type Options []Option

func (s Options) Config() Config {
	cfg := DefaultConfig(context.Background())
	s.apply(&cfg)
	return cfg
}

func (s Options) apply(cfg *Config) {
	for _, opt := range s {
		opt.apply(cfg)
	}
}

var DefaultConfig = func(ctx context.Context) Config {
	return Config{
		JitterBufDuration:     time.Second,
		CatchupMaxSpeedFactor: 10,
		MaxCatchupAtLag:       21 * time.Second,
		ReadTimeout:           10 * time.Second,
	}
}

type OptionJitterBufDuration time.Duration

func (s OptionJitterBufDuration) apply(cfg *Config) {
	cfg.JitterBufDuration = time.Duration(s)
}

type OptionCatchupMaxSpeedFactor float64

func (s OptionCatchupMaxSpeedFactor) apply(cfg *Config) {
	cfg.CatchupMaxSpeedFactor = float64(s)
}

type OptionMaxCatchupAtLag time.Duration

func (s OptionMaxCatchupAtLag) apply(cfg *Config) {
	cfg.MaxCatchupAtLag = time.Duration(s)
}

type OptionReadTimeout time.Duration

func (s OptionReadTimeout) apply(cfg *Config) {
	cfg.ReadTimeout = time.Duration(s)
}
