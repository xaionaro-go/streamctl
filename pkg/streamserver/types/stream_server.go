package types

import (
	"io"
	"strings"

	"github.com/xaionaro-go/streamctl/pkg/streamplayer"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types/streamportserver"
)

type PubsubNameser interface {
	PubsubNames() (AppKeys, error)
}

type Publisher = streamplayer.Publisher
type WaitPublisherChaner = streamplayer.WaitPublisherChaner
type GetPortServerser = streamportserver.GetPortServerser

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
