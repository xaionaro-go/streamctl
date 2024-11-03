package types

import (
	"context"
	"io"
)

type P2P interface {
	io.Closer
	Start(context.Context) error
	GetNetworkID() string
	GetPSK() []byte
	GetPeers() ([]Peer, error)
}
