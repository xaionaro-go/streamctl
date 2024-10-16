package types

import (
	"io"

	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

type PortServer interface {
	io.Closer

	Config() ServerConfig

	Type() streamtypes.ServerType
	ListenAddr() string

	NumBytesConsumerWrote() uint64
	NumBytesProducerRead() uint64
}

type StreamDestination struct {
	ID  DestinationID
	URL string
}

type StreamID = streamtypes.StreamID

type DestinationID = streamtypes.DestinationID

type AppKey string

type AppKeys []AppKey

func (s AppKeys) Strings() []string {
	result := make([]string, 0, len(s))
	for _, in := range s {
		result = append(result, string(in))
	}
	return result
}
