package types

import (
	"io"

	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

type PortServer interface {
	io.Closer

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

type DestinationID string
