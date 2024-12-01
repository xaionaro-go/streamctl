package types

import (
	"context"
	"net"

	"google.golang.org/grpc"
)

type Peer interface {
	GetID() PeerID
	GetName() string
	DialContext(ctx context.Context, network string, addr string) (net.Conn, error)
	GRPCClient() *grpc.ClientConn
}
