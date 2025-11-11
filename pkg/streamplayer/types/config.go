package types

import (
	"context"
	"time"

	playertypes "github.com/xaionaro-go/player/pkg/player/types"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

type FuncNotifyStart func(ctx context.Context, streamID streamtypes.StreamID)
type GetRestartChanFunc func() <-chan struct{}

type Config struct {
	JitterBufMaxDuration  time.Duration
	JitterBufMinDuration  time.Duration
	CatchupMaxSpeedFactor float64
	CatchupAtMaxLag       time.Duration
	StartTimeout          time.Duration
	ReadTimeout           time.Duration
	NotifierStart         []FuncNotifyStart `yaml:"-"`
	OverrideURL           string
	GetRestartChanFunc    GetRestartChanFunc   `yaml:"-"`
	CustomPlayerOptions   []playertypes.Option `yaml:"-"`
	ForceWaitForPublisher bool
	EnableObserver        bool
}

func (cfg Config) Options() Options {
	var opts Options
	if cfg.JitterBufMaxDuration != 0 {
		opts = append(opts, OptionJitterBufMaxDuration(cfg.JitterBufMaxDuration))
	}
	if cfg.JitterBufMinDuration != 0 {
		opts = append(opts, OptionJitterBufMinDuration(cfg.JitterBufMinDuration))
	}
	if cfg.CatchupMaxSpeedFactor != 0 {
		opts = append(opts, OptionCatchupMaxSpeedFactor(cfg.CatchupMaxSpeedFactor))
	}
	if cfg.CatchupAtMaxLag != 0 {
		opts = append(opts, OptionMaxCatchupAtLag(cfg.CatchupAtMaxLag))
	}
	if cfg.StartTimeout != 0 {
		opts = append(opts, OptionStartTimeout(cfg.StartTimeout))
	}
	if cfg.ReadTimeout != 0 {
		opts = append(opts, OptionReadTimeout(cfg.ReadTimeout))
	}
	if cfg.NotifierStart != nil {
		opts = append(opts, OptionNotifierStart(cfg.NotifierStart))
	}
	if cfg.OverrideURL != "" {
		opts = append(opts, OptionOverrideURL(cfg.OverrideURL))
	}
	if cfg.GetRestartChanFunc != nil {
		opts = append(opts, OptionGetRestartChanFunc(cfg.GetRestartChanFunc))
	}
	if cfg.CustomPlayerOptions != nil {
		opts = append(opts, OptionCustomPlayerOptions(cfg.CustomPlayerOptions))
	}
	if cfg.ForceWaitForPublisher {
		opts = append(opts, OptionForceWaitForPublisher(cfg.ForceWaitForPublisher))
	}
	if cfg.EnableObserver {
		opts = append(opts, OptionEnableObserver(cfg.EnableObserver))
	}
	return opts
}

type Option interface {
	Apply(cfg *Config)
}

type Options []Option

func (s Options) Config(ctx context.Context) Config {
	cfg := DefaultConfig(ctx)
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
		JitterBufMinDuration:  200 * time.Millisecond,
		JitterBufMaxDuration:  3 * time.Minute,
		CatchupMaxSpeedFactor: 1.1,
		CatchupAtMaxLag:       3 * time.Minute,
		StartTimeout:          10 * time.Second,
		ReadTimeout:           10 * time.Second,
	}
}

type OptionJitterBufMaxDuration time.Duration

func (s OptionJitterBufMaxDuration) Apply(cfg *Config) {
	cfg.JitterBufMaxDuration = time.Duration(s)
}

type OptionJitterBufMinDuration time.Duration

func (s OptionJitterBufMinDuration) Apply(cfg *Config) {
	cfg.JitterBufMinDuration = time.Duration(s)
}

type OptionCatchupMaxSpeedFactor float64

func (s OptionCatchupMaxSpeedFactor) Apply(cfg *Config) {
	cfg.CatchupMaxSpeedFactor = float64(s)
}

type OptionMaxCatchupAtLag time.Duration

func (s OptionMaxCatchupAtLag) Apply(cfg *Config) {
	cfg.CatchupAtMaxLag = time.Duration(s)
}

type OptionStartTimeout time.Duration

func (s OptionStartTimeout) Apply(cfg *Config) {
	cfg.StartTimeout = time.Duration(s)
}

type OptionReadTimeout time.Duration

func (s OptionReadTimeout) Apply(cfg *Config) {
	cfg.ReadTimeout = time.Duration(s)
}

type OptionNotifierStart []FuncNotifyStart

func (s OptionNotifierStart) Apply(cfg *Config) {
	cfg.NotifierStart = ([]FuncNotifyStart)(s)
}

type OptionOverrideURL string

func (s OptionOverrideURL) Apply(cfg *Config) {
	cfg.OverrideURL = string(s)
}

type OptionGetRestartChanFunc func() <-chan struct{}

func (s OptionGetRestartChanFunc) Apply(cfg *Config) {
	cfg.GetRestartChanFunc = GetRestartChanFunc(s)
}

type OptionCustomPlayerOptions playertypes.Options

func (s OptionCustomPlayerOptions) Apply(cfg *Config) {
	cfg.CustomPlayerOptions = playertypes.Options(s)
}

type OptionForceWaitForPublisher bool

func (s OptionForceWaitForPublisher) Apply(cfg *Config) {
	cfg.ForceWaitForPublisher = bool(s)
}

type OptionEnableObserver bool

func (s OptionEnableObserver) Apply(cfg *Config) {
	cfg.EnableObserver = bool(s)
}
