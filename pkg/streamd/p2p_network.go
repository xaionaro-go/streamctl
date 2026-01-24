package streamd

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/p2p"
	p2ptypes "github.com/xaionaro-go/streamctl/pkg/p2p/types"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/client"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
)

func (d *StreamD) initP2P(
	ctx context.Context,
) error {
	fmt.Printf("initP2P: Enable=%v\n", d.Config.P2PNetwork.Enable)
	if !d.Config.P2PNetwork.Enable {
		return nil
	}
	if d.Config.P2PNetwork.IsZero() {
		d.Config.P2PNetwork = config.GetRandomP2PConfig()
		if err := d.saveConfig(ctx); err != nil {
			logger.Errorf(ctx, "unable to save the config: %v", err)
		}
	}

	privKey, err := d.Config.P2PNetwork.PrivateKey.Get()
	if err != nil {
		return fmt.Errorf("unable to get the private key: %w", err)
	}

	p2p, err := p2p.NewP2P(
		ctx,
		privKey,
		d.Config.P2PNetwork.PeerName,
		d.Config.P2PNetwork.NetworkID,
		[]byte(d.Config.P2PNetwork.PSK.Get()),
		d.Config.P2PNetwork.VPN.Network,
		d.Options.P2PSetupServer,
		d.Options.P2PSetupClient,
	)
	if err != nil {
		return fmt.Errorf("unable to initialize a P2P network handler: %w", err)
	}

	err = p2p.Start(ctx)
	if err != nil {
		return fmt.Errorf("unable to start a P2P network handler: %w", err)
	}
	d.addCloseCallback(p2p.Close, "P2P network")

	d.P2PNetwork = p2p
	return nil
}

func (d *StreamD) P2P() p2p.P2P {
	return d.P2PNetwork
}

func (d *StreamD) GetPeers() ([]p2ptypes.Peer, error) {
	return d.P2P().GetPeers()
}

func (d *StreamD) GetPeerIDs(ctx context.Context) ([]p2ptypes.PeerID, error) {
	peers, err := d.GetPeers()
	if err != nil {
		return nil, fmt.Errorf("unable to get the list of peers: %w", err)
	}

	var peerIDs []p2ptypes.PeerID
	for _, peer := range peers {
		peerIDs = append(peerIDs, peer.GetID())
	}
	return peerIDs, nil
}

func (d *StreamD) GetPeer(
	ctx context.Context,
	peerID p2ptypes.PeerID,
) (p2ptypes.Peer, error) {
	peers, err := d.GetPeers()
	if err != nil {
		return nil, fmt.Errorf("unable to get the list of peers: %w", err)
	}
	for _, peer := range peers {
		if peer.GetID().Equal(peerID) {
			return peer, nil
		}
	}
	return nil, fmt.Errorf("peer '%s' not found", peerID)
}

func (d *StreamD) DialPeerByID(
	ctx context.Context,
	peerID p2ptypes.PeerID,
) (api.StreamD, error) {
	peer, err := d.GetPeer(ctx, peerID)
	if err != nil {
		return nil, fmt.Errorf("unable to get peer with ID '%v': %w", peerID, err)
	}

	return client.WrapConn(ctx, peer.GRPCClient()), nil
}
