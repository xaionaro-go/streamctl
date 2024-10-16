package types

import (
	"context"
	"crypto"
	"crypto/x509"
	"io"
	"strings"
	"time"

	"github.com/xaionaro-go/streamctl/pkg/streamplayer"
)

type PubsubNameser interface {
	PubsubNames() (AppKeys, error)
}

type Publisher = streamplayer.Publisher
type WaitPublisherChaner = streamplayer.WaitPublisherChaner
type GetPortServerser = streamplayer.GetPortServerser

type InitConfig struct {
	DefaultStreamPlayerOptions streamplayer.Options
}

type InitOption interface {
	apply(*InitConfig)
}

type InitOptions []InitOption

func (s InitOptions) Config() InitConfig {
	cfg := InitConfig{}
	for _, opt := range s {
		opt.apply(&cfg)
	}
	return cfg
}

type InitOptionDefaultStreamPlayerOptions streamplayer.Options

func (opt InitOptionDefaultStreamPlayerOptions) apply(cfg *InitConfig) {
	cfg.DefaultStreamPlayerOptions = (streamplayer.Options)(opt)
}

type Sub interface {
	io.Closer
	ClosedChan() <-chan struct{}
}

func StreamID2LocalAppName(
	streamID StreamID,
) AppKey {
	streamIDParts := strings.Split(string(streamID), "/")
	localAppName := string(streamID)
	if len(streamIDParts) == 2 {
		localAppName = streamIDParts[1]
	}
	return AppKey(localAppName)
}

type IncomingStream struct {
	StreamID StreamID

	NumBytesWrote uint64
	NumBytesRead  uint64
}

type ServerOption interface {
	apply(*ServerConfig)
}

type ServerOptions []ServerOption

func (s ServerOptions) apply(cfg *ServerConfig) {
	for _, opt := range s {
		opt.apply(cfg)
	}
}

func (s ServerOptions) Config(
	ctx context.Context,
) ServerConfig {
	cfg := DefaultServerConfig(ctx)
	for _, opt := range s {
		opt.apply(&cfg)
	}
	return cfg
}

type ServerConfig struct {
	IsTLS          bool
	WriteQueueSize uint64
	WriteTimeout   time.Duration
	ReadTimeout    time.Duration
	ServerCert     *x509.Certificate
	ServerKey      crypto.PrivateKey
}

var DefaultServerConfig = func(
	ctx context.Context,
) ServerConfig {
	return ServerConfig{
		IsTLS:          false,
		WriteQueueSize: 60 * 10, // 60 FPS * 10 secs
		WriteTimeout:   10 * time.Second,
		ReadTimeout:    10 * time.Second,
		ServerCert:     nil,
		ServerKey:      nil,
	}
}

func (cfg ServerConfig) Options() ServerOptions {
	return ServerOptions{
		ServerOptionIsTLS((bool)(cfg.IsTLS)),
		ServerOptionWriteQueueSize((uint64)(cfg.WriteQueueSize)),
		ServerOptionWriteTimeout((time.Duration)(cfg.WriteTimeout)),
		ServerOptionReadTimeout((time.Duration)(cfg.ReadTimeout)),
		ServerOptionServerCert{cfg.ServerCert},
		ServerOptionServerKey{cfg.ServerKey},
	}
}

type ServerOptionIsTLS bool

func (opt ServerOptionIsTLS) apply(cfg *ServerConfig) {
	cfg.IsTLS = (bool)(opt)
}

type ServerOptionWriteQueueSize uint64

func (opt ServerOptionWriteQueueSize) apply(cfg *ServerConfig) {
	cfg.WriteQueueSize = (uint64)(opt)
}

type ServerOptionWriteTimeout time.Duration

func (opt ServerOptionWriteTimeout) apply(cfg *ServerConfig) {
	cfg.WriteTimeout = (time.Duration)(opt)
}

type ServerOptionReadTimeout time.Duration

func (opt ServerOptionReadTimeout) apply(cfg *ServerConfig) {
	cfg.ReadTimeout = (time.Duration)(opt)
}

type ServerOptionServerCert struct{ *x509.Certificate }

func (opt ServerOptionServerCert) apply(cfg *ServerConfig) {
	cfg.ServerCert = opt.Certificate
}

type ServerOptionServerKey struct{ crypto.PrivateKey }

func (opt ServerOptionServerKey) apply(cfg *ServerConfig) {
	if opt.PrivateKey == nil {
		cfg.ServerKey = nil
		return
	}
	cfg.ServerKey = opt.PrivateKey
}
