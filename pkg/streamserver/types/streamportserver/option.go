package streamportserver

import (
	"context"
	"crypto"
	"crypto/x509"
	"time"

	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

type Option interface {
	apply(*ProtocolSpecificConfig)
}

type Options []Option

func (s Options) apply(cfg *ProtocolSpecificConfig) {
	for _, opt := range s {
		opt.apply(cfg)
	}
}

func (s Options) ProtocolSpecificConfig(
	ctx context.Context,
) ProtocolSpecificConfig {
	cfg := DefaultConfig(ctx)
	for _, opt := range s {
		opt.apply(&cfg)
	}
	return cfg
}

type ProtocolSpecificConfig struct {
	IsTLS          bool
	WriteQueueSize uint64
	WriteTimeout   time.Duration
	ReadTimeout    time.Duration
	ServerCert     *x509.Certificate
	ServerKey      crypto.PrivateKey
}

var DefaultConfig = func(
	ctx context.Context,
) ProtocolSpecificConfig {
	return ProtocolSpecificConfig{
		IsTLS:          false,
		WriteQueueSize: 60 * 10, // 60 FPS * 10 secs
		WriteTimeout:   10 * time.Second,
		ReadTimeout:    10 * time.Second,
		ServerCert:     nil,
		ServerKey:      nil,
	}
}

func (cfg ProtocolSpecificConfig) Options() Options {
	return Options{
		OptionIsTLS((bool)(cfg.IsTLS)),
		OptionWriteQueueSize((uint64)(cfg.WriteQueueSize)),
		OptionWriteTimeout((time.Duration)(cfg.WriteTimeout)),
		OptionReadTimeout((time.Duration)(cfg.ReadTimeout)),
		OptionServerCert{cfg.ServerCert},
		OptionServerKey{cfg.ServerKey},
	}
}

type OptionIsTLS bool

func (opt OptionIsTLS) apply(cfg *ProtocolSpecificConfig) {
	cfg.IsTLS = (bool)(opt)
}

type OptionWriteQueueSize uint64

func (opt OptionWriteQueueSize) apply(cfg *ProtocolSpecificConfig) {
	cfg.WriteQueueSize = (uint64)(opt)
}

type OptionWriteTimeout time.Duration

func (opt OptionWriteTimeout) apply(cfg *ProtocolSpecificConfig) {
	cfg.WriteTimeout = (time.Duration)(opt)
}

type OptionReadTimeout time.Duration

func (opt OptionReadTimeout) apply(cfg *ProtocolSpecificConfig) {
	cfg.ReadTimeout = (time.Duration)(opt)
}

type OptionServerCert struct{ *x509.Certificate }

func (opt OptionServerCert) apply(cfg *ProtocolSpecificConfig) {
	cfg.ServerCert = opt.Certificate
}

type OptionServerKey struct{ crypto.PrivateKey }

func (opt OptionServerKey) apply(cfg *ProtocolSpecificConfig) {
	if opt.PrivateKey == nil {
		cfg.ServerKey = nil
		return
	}
	cfg.ServerKey = opt.PrivateKey
}

type Config struct {
	ProtocolSpecificConfig `yaml:"config"`
	Type                   streamtypes.ServerType `yaml:"protocol"`
	ListenAddr             string                 `yaml:"listen"`
}
