package client

import (
	"context"
	"time"

	"google.golang.org/grpc"
)

type ReconnectConfig struct {
	InitialInterval    time.Duration
	MaximalInterval    time.Duration
	IntervalMultiplier float64
}

type CallWrapperFunc func(
	ctx context.Context,
	req any,
	callFunc func(ctx context.Context, opts ...grpc.CallOption) error,
	opts ...grpc.CallOption,
) error

type ConnectWrapperFunc func(
	ctx context.Context,
	connectFunc func(ctx context.Context, opts ...grpc.DialOption) (*grpc.ClientConn, error),
	opts ...grpc.DialOption,
) (*grpc.ClientConn, error)

type Config struct {
	UsePersistentConnection bool
	CallWrapper             CallWrapperFunc
	ConnectWrapper          ConnectWrapperFunc
	Reconnect               ReconnectConfig
}

var DefaultConfig = func(ctx context.Context) Config {
	return Config{
		UsePersistentConnection: true,
		Reconnect: ReconnectConfig{
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

type OptionCallWrapper CallWrapperFunc

func (opt OptionCallWrapper) Apply(cfg *Config) {
	cfg.CallWrapper = CallWrapperFunc(opt)
}

type OptionConnectWrapper ConnectWrapperFunc

func (opt OptionConnectWrapper) Apply(cfg *Config) {
	cfg.ConnectWrapper = ConnectWrapperFunc(opt)
}

type OptionReconnectInitialInterval time.Duration

func (opt OptionReconnectInitialInterval) Apply(cfg *Config) {
	cfg.Reconnect.InitialInterval = time.Duration(opt)
}

type OptionReconnectMaximalInterval time.Duration

func (opt OptionReconnectMaximalInterval) Apply(cfg *Config) {
	cfg.Reconnect.MaximalInterval = time.Duration(opt)
}

type OptionReconnectIntervalMultiplier float64

func (opt OptionReconnectIntervalMultiplier) Apply(cfg *Config) {
	cfg.Reconnect.IntervalMultiplier = float64(opt)
}
