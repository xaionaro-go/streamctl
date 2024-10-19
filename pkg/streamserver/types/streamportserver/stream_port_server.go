package streamportserver

import (
	"io"

	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

type Server interface {
	io.Closer

	ProtocolSpecificConfig() ProtocolSpecificConfig

	Type() streamtypes.ServerType
	ListenAddr() string

	NumBytesConsumerWrote() uint64
	NumBytesProducerRead() uint64
}
