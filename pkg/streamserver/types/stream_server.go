package types

import (
	"context"
	"crypto"
	"crypto/x509"
	"io"
	"strings"
	"time"

	"github.com/xaionaro-go/streamctl/pkg/player"
	"github.com/xaionaro-go/streamctl/pkg/streamplayer"
	sptypes "github.com/xaionaro-go/streamctl/pkg/streamplayer/types"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

type PubsubNameser interface {
	PubsubNames() AppKeys
}

type Publisher = streamplayer.Publisher
type WaitPublisherChaner = streamplayer.WaitPublisherChaner
type GetPortServerser = streamplayer.GetPortServerser

type StreamServer[AF any] interface {
	streamplayer.StreamServer
	PubsubNameser

	Init(
		ctx context.Context,
		opts ...InitOption,
	) error

	StartServer(
		ctx context.Context,
		serverType streamtypes.ServerType,
		listenAddr string,
		opts ...ServerOption,
	) (PortServer, error)
	StopServer(
		ctx context.Context,
		server PortServer,
	) error

	AddIncomingStream(
		ctx context.Context,
		streamID StreamID,
	) error
	ListIncomingStreams(
		ctx context.Context,
	) []IncomingStream
	RemoveIncomingStream(
		ctx context.Context,
		streamID StreamID,
	) error

	ListStreamDestinations(
		ctx context.Context,
	) ([]StreamDestination, error)
	AddStreamDestination(
		ctx context.Context,
		destinationID DestinationID,
		url string,
	) error
	RemoveStreamDestination(
		ctx context.Context,
		destinationID DestinationID,
	) error

	AddStreamForward(
		ctx context.Context,
		streamID StreamID,
		destinationID DestinationID,
		enabled bool,
		quirks ForwardingQuirks,
	) (*StreamForward[AF], error)
	ListStreamForwards(
		ctx context.Context,
	) ([]StreamForward[AF], error)
	UpdateStreamForward(
		ctx context.Context,
		streamID StreamID,
		destinationID DestinationID,
		enabled bool,
		quirks ForwardingQuirks,
	) (*StreamForward[AF], error)
	RemoveStreamForward(
		ctx context.Context,
		streamID StreamID,
		dstID DestinationID,
	) error

	AddStreamPlayer(
		ctx context.Context,
		streamID StreamID,
		playerType player.Backend,
		disabled bool,
		streamPlaybackConfig sptypes.Config,
		opts ...StreamPlayerOption,
	) error
	UpdateStreamPlayer(
		ctx context.Context,
		streamID StreamID,
		playerType player.Backend,
		disabled bool,
		streamPlaybackConfig sptypes.Config,
		opts ...StreamPlayerOption,
	) error
	RemoveStreamPlayer(
		ctx context.Context,
		streamID StreamID,
	) error
	ListStreamPlayers(
		ctx context.Context,
	) ([]StreamPlayer, error)
	GetStreamPlayer(
		ctx context.Context,
		streamID StreamID,
	) (*StreamPlayer, error)
	GetActiveStreamPlayer(
		ctx context.Context,
		streamID StreamID,
	) (player.Player, error)

	ListServers(ctx context.Context) []PortServer
}

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
	cfg.ServerKey = (crypto.PrivateKey)(opt)
}
