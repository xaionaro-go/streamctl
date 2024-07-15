package types

import (
	"fmt"
	"io"
)

type ServerType int

const (
	ServerTypeUndefined = ServerType(iota)
	ServerTypeRTMP
	ServerTypeRTSP
)

func (t ServerType) String() string {
	switch t {
	case ServerTypeUndefined:
		return "<undefined>"
	case ServerTypeRTMP:
		return "rtmp"
	case ServerTypeRTSP:
		return "rtsp"
	default:
		return fmt.Sprintf("unexpected_type_%d", t)
	}
}

type ServerHandler interface {
	io.Closer

	Type() ServerType
	ListenAddr() string
}

type StreamDestination struct {
	ID  DestinationID
	URL string
}

type StreamID string

type DestinationID string
