package weron

import (
	"context"
	"fmt"
	"net"

	"github.com/pojntfx/weron/pkg/wrtcconn"
	"github.com/xaionaro-go/streamctl/pkg/p2p/types"
)

type Peer struct {
	id      types.PeerID
	name    string
	network *P2P
	*peerServer
	*peerClient
}

var _ types.Peer = (*Peer)(nil)

func newPeer(id types.PeerID, network *P2P) *Peer {
	return &Peer{
		id:      id,
		network: network,
	}
}

func (p *Peer) initServer(
	ctx context.Context,
	wrtcPeer *wrtcconn.Peer,
) error {
	peerServer := newPeerServer(p, wrtcPeer)
	if err := peerServer.init(ctx); err != nil {
		return err
	}
	p.peerServer = peerServer
	return nil
}

func (p *Peer) initClient(
	ctx context.Context,
	wrtcPeer *wrtcconn.Peer,
) error {
	peerClient := newPeerClient(p, wrtcPeer)
	if err := peerClient.init(ctx); err != nil {
		return err
	}
	p.peerClient = peerClient
	return nil
}

func (p *Peer) GetID() types.PeerID {
	return p.id
}

func (p *Peer) GetName() string {
	return p.name
}

func (p *Peer) DialContext(
	ctx context.Context,
	network string,
	addr string,
) (net.Conn, error) {
	if p.peerClient == nil {
		return nil, fmt.Errorf("the client connection is not initialized yet")
	}
	return p.peerClient.DialContext(ctx, network, addr)
}
