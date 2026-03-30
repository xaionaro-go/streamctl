package types

import (
	"io"
	"strings"

	sptypes "github.com/xaionaro-go/streamctl/pkg/streamplayer/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types/streamportserver"
)

type ActiveIncomingStreamIDser interface {
	ActiveIncomingStreamIDs() ([]StreamID, error)
}

type Publisher = sptypes.Publisher
type WaitPublisherChaner = sptypes.WaitPublisherChaner
type GetPortServerser = streamportserver.GetPortServerser

type InitConfig struct {
	DefaultStreamPlayerOptions sptypes.Options
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

type InitOptionDefaultStreamPlayerOptions sptypes.Options

func (opt InitOptionDefaultStreamPlayerOptions) apply(cfg *InitConfig) {
	cfg.DefaultStreamPlayerOptions = (sptypes.Options)(opt)
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
