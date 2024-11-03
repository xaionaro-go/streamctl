package types

import (
	"context"
	"net"
)

type Peer interface {
	GetID() PeerID
	GetName() string
	DialContext(ctx context.Context, network string, addr string) (net.Conn, error)
}
