package weron

import (
	"context"
	"fmt"
	"net"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/pojntfx/weron/pkg/wrtcconn"
	"github.com/xaionaro-go/grpcproxy/grpchttpproxy"
	"github.com/xaionaro-go/grpcproxy/protobuf/go/proxy_grpc"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/p2p/implementations/weron/protobuf/go/p2p_grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type peerClient struct {
	peer        *Peer
	wrtcPeer    *wrtcconn.Peer
	grpcConn    *grpc.ClientConn
	peerClient  p2p_grpc.PeerClient
	proxyClient proxy_grpc.NetworkProxyClient
	cancelFn    context.CancelFunc
}

func newPeerClient(
	peer *Peer,
	wrtcPeer *wrtcconn.Peer,
) *peerClient {
	return &peerClient{
		peer:     peer,
		wrtcPeer: wrtcPeer,
	}
}

func (p *peerClient) init(
	ctx context.Context,
) error {
	ctx, cancelFn := context.WithCancel(ctx)
	p.cancelFn = cancelFn
	observability.Go(ctx, func() {
		<-ctx.Done()
		if err := p.peer.network.removePeer(p.peer.id); err != nil {
			logger.Errorf(ctx, "unable to remove peer '%s': %v", p.peer.id, err)
		}
		if err := p.wrtcPeer.Conn.Close(); err != nil {
			logger.Errorf(ctx, "unable to close peer '%s': %v", p.peer.id, err)
		}
	})

	grpcConn, err := grpc.NewClient(
		"0.0.0.0:0",
		grpc.WithContextDialer(func(ctx context.Context, s string) (net.Conn, error) {
			return connFromReadWriter(p.wrtcPeer.Conn, &net.IPAddr{IP: net.ParseIP("0.0.0.0")}), nil
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(&peerClientStatsHandler{p}),
	)
	if err != nil {
		return fmt.Errorf("unable to create a gRPC client: %w", err)
	}
	p.grpcConn = grpcConn

	p.proxyClient = proxy_grpc.NewNetworkProxyClient(grpcConn)
	p.peerClient = p2p_grpc.NewPeerClient(grpcConn)
	if p.peer.network.setupClient != nil {
		if err := p.peer.network.setupClient(grpcConn); err != nil {
			return fmt.Errorf("unable to setup the client connection: %w", err)
		}
	}

	nameReply, err := p.peerClient.GetName(ctx, &p2p_grpc.GetNameRequest{})
	if err != nil {
		return fmt.Errorf("unable to request the peer name: %w", err)
	}
	p.peer.name = nameReply.GetName()
	logger.Debugf(ctx, "peer name: %s", p.peer.name)

	err = p.Ping(ctx, "")
	if err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}

	return nil
}

func (p *peerClient) Ping(
	ctx context.Context,
	payload string,
) error {
	reply, err := p.peerClient.Ping(ctx, &p2p_grpc.PingRequest{
		Payload: payload,
	})
	if err != nil {
		return err
	}
	if reply.GetPayload() != payload {
		return fmt.Errorf("the payload did not match: '%s' != '%s'", reply.GetPayload(), payload)
	}
	return nil
}

func (p *peerClient) Close() error {
	p.cancelFn()
	return nil
}

func (p *peerClient) DialContext(
	ctx context.Context,
	network string,
	addr string,
) (net.Conn, error) {
	return grpchttpproxy.NewDialer(p.proxyClient).DialContext(ctx, "tcp", addr)
}

func (p *peerClient) GRPCConn() *grpc.ClientConn {
	return p.grpcConn
}
