package client

import (
	"context"
	"time"

	"github.com/xaionaro-go/xgrpc"
	"google.golang.org/grpc"
)

type ConnectWrapperFunc func(
	ctx context.Context,
	connectFunc func(ctx context.Context, opts ...grpc.DialOption) (*grpc.ClientConn, error),
	opts ...grpc.DialOption,
) (*grpc.ClientConn, error)

type Config struct {
	UsePersistentConnection bool
	CallWrapper             xgrpc.CallWrapperFunc
	ConnectWrapper          ConnectWrapperFunc
	Retry                   xgrpc.RetryConfig
}

var DefaultConfig = func(ctx context.Context) Config {
	return Config{
		UsePersistentConnection: true,
		Retry: xgrpc.RetryConfig{
			InitialInterval:    200 * time.Millisecond,
			MaximalInterval:    5 * time.Second,
			IntervalMultiplier: 1.1,
		},
	}
}

type Option interface {
	Apply(*Config)
}

type Options []Option

func (s Options) Apply(cfg *Config) {
	for _, opt := range s {
		opt.Apply(cfg)
	}
}

func (s Options) Config(ctx context.Context) Config {
	cfg := DefaultConfig(ctx)
	s.Apply(&cfg)
	return cfg
}

type OptionUsePersistentConnection bool

func (opt OptionUsePersistentConnection) Apply(cfg *Config) {
	cfg.UsePersistentConnection = bool(opt)
}

type OptionCallWrapper xgrpc.CallWrapperFunc

func (opt OptionCallWrapper) Apply(cfg *Config) {
	cfg.CallWrapper = xgrpc.CallWrapperFunc(opt)
}

type OptionConnectWrapper ConnectWrapperFunc

func (opt OptionConnectWrapper) Apply(cfg *Config) {
	cfg.ConnectWrapper = ConnectWrapperFunc(opt)
}

type OptionReconnectInitialInterval time.Duration

func (opt OptionReconnectInitialInterval) Apply(cfg *Config) {
	cfg.Retry.InitialInterval = time.Duration(opt)
}

type OptionReconnectMaximalInterval time.Duration

func (opt OptionReconnectMaximalInterval) Apply(cfg *Config) {
	cfg.Retry.MaximalInterval = time.Duration(opt)
}

type OptionReconnectIntervalMultiplier float64

func (opt OptionReconnectIntervalMultiplier) Apply(cfg *Config) {
	cfg.Retry.IntervalMultiplier = float64(opt)
}
