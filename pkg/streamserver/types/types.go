package types

import "io"

type ServerType int

const (
	ServerTypeUndefined = ServerType(iota)
	ServerTypeRTMP
	ServerTypeRTSP
)

type ServerHandler interface {
	io.Closer

	Type() ServerType
	ListenAddr() string
}

type StreamDestination struct {
	StreamID StreamID
	URL      string
}

type StreamID string
