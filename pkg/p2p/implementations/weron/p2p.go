package weron

import (
	"context"
	"crypto"
	"crypto/aes"
	"crypto/ed25519"
	"crypto/sha512"
	"errors"
	"fmt"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/pojntfx/weron/pkg/services"
	"github.com/pojntfx/weron/pkg/wrtcconn"
	"github.com/pojntfx/weron/pkg/wrtcip"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/consts"
	p2pconsts "github.com/xaionaro-go/streamctl/pkg/p2p/implementations/weron/consts"
	"github.com/xaionaro-go/streamctl/pkg/p2p/types"
	"github.com/xaionaro-go/streamctl/pkg/secret"
)

const (
	salt = "this is not important what is here, important is that this is not an empty (and pretty unique) string"
)

type P2P struct {
	startCount   atomic.Uint32
	locker       sync.Mutex
	waitGroup    sync.WaitGroup
	networkID    string
	peerName     string
	setupServer  types.FuncSetupServer
	setupClient  types.FuncSetupClient
	vpnAdapter   *wrtcip.Adapter
	conn0Adapter *wrtcconn.Adapter
	conn1Adapter *wrtcconn.Adapter
	privKey      secret.Any[ed25519.PrivateKey]
	psk          secret.Any[[]byte]
	peers        map[string]*Peer
	cancelFn     context.CancelFunc
}

var _ types.P2P = (*P2P)(nil)

func getSignalerURL(
	networkID string,
	serviceID string,
	signallerPassword string,
) (*url.URL, error) {
	u, err := url.Parse(p2pconsts.SignalerURL)
	if err != nil {
		return nil, fmt.Errorf("unable to parse the initial URL: %w", err)
	}

	q := u.Query()
	q.Set("community", networkID+"-"+serviceID)
	q.Set("password", signallerPassword)
	u.RawQuery = q.Encode()
	return u, nil
}

func getICEServers() []string {
	// TODO: implement me
	return []string{"stun:stun.l.google.com:19302"}
}

var p2pCount atomic.Uint32

func NewP2P(
	ctx context.Context,
	privKey crypto.PrivateKey,
	peerName string,
	networkID string,
	psk []byte,
	networkCIDR string,
	setupServer types.FuncSetupServer,
	setupClient types.FuncSetupClient,
) (*P2P, error) {
	if len(psk) != aes.BlockSize {
		return nil, fmt.Errorf("expected a Pre-Shared-Key of size 16, received %d", len(psk))
	}

	ed25519PrivKey, ok := privKey.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("we currently support only ED25519 private keys, but received %T", privKey)
	}
	if len(ed25519PrivKey) == 0 {
		return nil, fmt.Errorf("the private key is empty")
	}

	h := sha512.New512_256()
	h.Write(psk)
	h.Write([]byte(salt))
	signalerPassword := h.Sum(nil)

	p := &P2P{
		networkID: networkID,
		privKey:   secret.New(ed25519PrivKey),
		peerName:  peerName,
		peers:     map[string]*Peer{},
	}
	signalerURLVPN, err := getSignalerURL(
		networkID,
		"vpn",
		string(signalerPassword),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to get a signaler URL for VPN: %w", err)
	}
	logger.Debugf(ctx, "VPN signaler URL: %s", signalerURLVPN)

	signalerURLConn0, err := getSignalerURL(
		networkID,
		"conn0",
		string(signalerPassword),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to get a signaler URL for Conn0: %w", err)
	}
	logger.Debugf(ctx, "Conn0 signaler URL: %s", signalerURLConn0)

	signalerURLConn1, err := getSignalerURL(
		networkID,
		"conn1",
		string(signalerPassword),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to get a signaler URL for Conn1: %w", err)
	}
	logger.Debugf(ctx, "Conn1 signaler URL: %s", signalerURLConn0)

	iceServers := getICEServers()

	vpnAdapter := wrtcip.NewAdapter(
		signalerURLVPN.String(), // TODO: replace the server-based signaler with a DHT-based signaler
		string(psk),             // TODO: use ECDH to generate an ephemeral encryption key
		iceServers,
		&wrtcip.AdapterConfig{
			Device:             fmt.Sprintf("%s%d", strings.ToLower(consts.AppName), p2pCount.Add(1)),
			OnSignalerConnect:  p.onVPNSignalerConnect,
			OnPeerConnect:      p.onVPNPeerConnect,
			OnPeerDisconnected: p.onVPNPeerDisconnected,
			CIDRs:              []string{networkCIDR},
			MaxRetries:         255,                   // TODO: pass this as an option
			Parallel:           runtime.GOMAXPROCS(0), // TODO: pass this as an option
			NamedAdapterConfig: &wrtcconn.NamedAdapterConfig{
				AdapterConfig: &wrtcconn.AdapterConfig{
					Timeout:             10 * time.Second, // TODO: pass this as an option
					ForceRelay:          false,            // TODO: pass this as an option
					OnSignalerReconnect: p.onVPNSignalerReconnect,
				},
				IDChannel: services.IPID,
				Kicks:     5 * time.Second, // TODO: pass this as an option
			},
			Static: true, // TODO: pass this as an option
		},
		ctx,
	)
	p.vpnAdapter = vpnAdapter

	conn0Adapter := wrtcconn.NewAdapter(
		signalerURLConn0.String(), // TODO: replace the server-based signaler with a DHT-based signaler
		string("test"),            // TODO: use ECDH to generate an ephemeral encryption key
		iceServers,
		[]string{"streampanel/connection_bus_0"},
		&wrtcconn.AdapterConfig{
			Timeout:             10 * time.Second, // TODO: pass this as an option
			ID:                  p.GetPeerID().String(),
			ForceRelay:          false, // TODO: pass this as an option
			OnSignalerReconnect: p.onConn0SignalerReconnect,
		},
		ctx,
	)
	p.conn0Adapter = conn0Adapter

	conn1Adapter := wrtcconn.NewAdapter(
		signalerURLConn1.String(), // TODO: replace the server-based signaler with a DHT-based signaler
		string(psk),               // TODO: use ECDH to generate an ephemeral encryption key
		iceServers,
		[]string{"streampanel/connection_bus_1"},
		&wrtcconn.AdapterConfig{
			Timeout:             10 * time.Second, // TODO: pass this as an option
			ID:                  p.GetPeerID().String(),
			ForceRelay:          false, // TODO: pass this as an option
			OnSignalerReconnect: p.onConn1SignalerReconnect,
		},
		ctx,
	)
	p.conn1Adapter = conn1Adapter

	return p, nil
}

func (p *P2P) Start(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "Start")
	defer func() { logger.Debugf(ctx, "/Start: %v", _err) }()
	if p.startCount.Add(1) > 1 {
		return fmt.Errorf("Start could be called only once")
	}
	ctx, p.cancelFn = context.WithCancel(ctx)

	if err := p.vpnAdapter.Open(); err != nil {
		logger.Errorf(ctx, "unable to initialize the VPN: %v", err)
	}

	myIDChan0, err := p.conn0Adapter.Open()
	if err != nil {
		return fmt.Errorf("unable to initialize the P2P connection 0 adapter: %w", err)
	}

	myIDChan1, err := p.conn1Adapter.Open()
	if err != nil {
		return fmt.Errorf("unable to initialize the P2P connection 1 adapter: %w", err)
	}

	p.waitGroup.Add(1)
	observability.Go(ctx, func() {
		defer p.waitGroup.Done()
		err := p.acceptPeersLoop(ctx, 0, myIDChan0)
		if err != nil && !errors.Is(err, context.Canceled) {
			logger.Errorf(ctx, "the peers accepting loop for conn 0 returned an error: %v", err)
		}
	})

	p.waitGroup.Add(1)
	observability.Go(ctx, func() {
		defer p.waitGroup.Done()
		err := p.acceptPeersLoop(ctx, 1, myIDChan1)
		if err != nil && !errors.Is(err, context.Canceled) {
			logger.Errorf(ctx, "the peers accepting loop for conn 1 returned an error: %v", err)
		}
	})

	return nil
}

func (p *P2P) acceptPeersLoop(
	ctx context.Context,
	connID int,
	myIDChan <-chan string,
) (_err error) {
	logger.Debugf(ctx, "acceptPeersLoop")
	defer func() { logger.Debugf(ctx, "/acceptPeersLoop: %v", _err) }()

	var acceptChan chan *wrtcconn.Peer
	switch connID {
	case 0:
		acceptChan = p.conn0Adapter.Accept()
	case 1:
		acceptChan = p.conn1Adapter.Accept()
	default:
		return fmt.Errorf("unexpected conn ID: %d", connID)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case receivedID := <-myIDChan:
			logger.Debugf(ctx, "connected to the signaller with ID %v", receivedID)
			receivedID = strings.SplitN(receivedID, "-", 2)[0]
			if receivedID != p.GetPeerID().String() {
				return fmt.Errorf("received unexpected PeerID as my peer ID: %s != %s", receivedID, p.GetPeerID())
			}
		case wrtcPeer := <-acceptChan:
			_, err := p.newPeer(ctx, connID, wrtcPeer)
			if err != nil {
				logger.Errorf(ctx, "unable to initialize the peer %s: %v", wrtcPeer.PeerID, err)
			}
		}
	}
}

func (p *P2P) newPeer(
	ctx context.Context,
	connID int,
	wrtcPeer *wrtcconn.Peer,
) (_ret *Peer, _err error) {
	logger.Debugf(ctx, "newPeer(ctx, %d, Peer:%s)", connID, wrtcPeer.PeerID)
	defer func() { logger.Debugf(ctx, "/newPeer(ctx, %d, Peer:%s): %v", connID, wrtcPeer.PeerID, _err) }()

	peerID, err := types.ParsePeerID(wrtcPeer.PeerID)
	if err != nil {
		return nil, fmt.Errorf("unable to parse the peerID: %w", err)
	}

	p.locker.Lock()
	defer p.locker.Unlock()

	peer := p.peers[peerID.String()]
	if peer == nil {
		peer = newPeer(peerID, p)
		p.peers[peerID.String()] = peer
	}

	isServer := peerID.Less(p.GetPeerID()) == (connID == 0)
	if isServer {
		if err := peer.initServer(ctx, wrtcPeer); err != nil {
			return nil, fmt.Errorf("unable to initialize the peer server handler: %w", err)
		}
	} else {
		if err := peer.initClient(ctx, wrtcPeer); err != nil {
			return nil, fmt.Errorf("unable to initialize the peer client handler: %w", err)
		}
	}
	return peer, nil
}

func (p *P2P) removePeer(
	peerID types.PeerID,
) (_err error) {
	ctx := context.TODO()
	logger.Debugf(ctx, "removePeer")
	defer func() { logger.Debugf(ctx, "/removePeer: %v", _err) }()

	p.locker.Lock()
	defer p.locker.Unlock()
	peerIDString := peerID.String()
	if _, ok := p.peers[peerIDString]; !ok {
		return fmt.Errorf("peer %s not found", peerIDString)
	}
	delete(p.peers, peerIDString)
	return nil
}

func (p *P2P) Close() (_err error) {
	ctx := context.TODO()
	logger.Debugf(ctx, "Close")
	defer func() { logger.Debugf(ctx, "/Close: %v", _err) }()
	return p.vpnAdapter.Close()
}

func (p *P2P) onVPNSignalerConnect(myPeerID string) {
	ctx := context.TODO()
	logger.Debugf(ctx, "Close")
	defer func() { logger.Debugf(ctx, "/Close") }()
}

func (p *P2P) onVPNSignalerReconnect() {
	ctx := context.TODO()
	logger.Debugf(ctx, "onVPNSignalerReconnect")
	defer func() { logger.Debugf(ctx, "/onVPNSignalerReconnect") }()
}

func (p *P2P) onConn0SignalerReconnect() {
	ctx := context.TODO()
	logger.Debugf(ctx, "onConn0SignalerReconnect")
	defer func() { logger.Debugf(ctx, "/onConn0SignalerReconnect") }()
}

func (p *P2P) onConn1SignalerReconnect() {
	ctx := context.TODO()
	logger.Debugf(ctx, "onConn1SignalerReconnect")
	defer func() { logger.Debugf(ctx, "/onConn1SignalerReconnect") }()

}

func (p *P2P) onVPNPeerConnect(peerIDString string) {
	ctx := context.TODO()
	logger.Debugf(ctx, "onVPNPeerConnect")
	defer func() { logger.Debugf(ctx, "/onVPNPeerConnect") }()

}

func (p *P2P) onVPNPeerDisconnected(peerID string) {
	ctx := context.TODO()
	logger.Debugf(ctx, "onVPNPeerDisconnected")
	defer func() { logger.Debugf(ctx, "/onVPNPeerDisconnected") }()

}

func (p *P2P) GetPeerID() types.PeerID {
	privKey := p.privKey.Get()
	if privKey == nil {
		return nil
	}
	return types.PeerID(p.privKey.Get().Public().(ed25519.PublicKey))
}

func (p *P2P) GetNetworkID() string {
	return p.networkID
}

func (p *P2P) GetPSK() []byte {
	return p.psk.Get()
}

func (p *P2P) GetPeers() ([]types.Peer, error) {
	result := make([]types.Peer, 0, len(p.peers))
	for _, peer := range p.peers {
		result = append(result, peer)
	}
	return result, nil
}
