package weron

import (
	"context"
	"net"
	"sync"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/pojntfx/weron/pkg/wrtcconn"
	"github.com/xaionaro-go/grpcproxy/grpcproxyserver"
	"github.com/xaionaro-go/grpcproxy/protobuf/go/proxy_grpc"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/p2p/implementations/weron/protobuf/go/p2p_grpc"
	"google.golang.org/grpc"
)

type peerServer struct {
	peer      *Peer
	waitGroup sync.WaitGroup
	wrtcPeer  *wrtcconn.Peer
	p2p_grpc.UnimplementedPeerServer
}

func newPeerServer(
	peer *Peer,
	wrtcPeer *wrtcconn.Peer,
) *peerServer {
	return &peerServer{
		peer:     peer,
		wrtcPeer: wrtcPeer,
	}
}

func (p *peerServer) init(
	ctx context.Context,
) error {
	ctx, cancelFn := context.WithCancel(ctx)
	observability.Go(ctx, func() {
		<-ctx.Done()
		if err := p.peer.network.removePeer(p.peer.id); err != nil {
			logger.Errorf(ctx, "unable to remove peer '%s': %v", p.peer.id, err)
		}
		if err := p.wrtcPeer.Conn.Close(); err != nil {
			logger.Errorf(ctx, "unable to close peer '%s': %v", p.peer.id, err)
		}
	})

	grpcServerListener := newDummyListener(ctx, connFromReadWriter(p.wrtcPeer.Conn, &net.IPAddr{IP: net.ParseIP("0.0.0.0")}))
	grpcServer := grpc.NewServer()
	proxyServer := grpcproxyserver.New()
	proxy_grpc.RegisterNetworkProxyServer(grpcServer, proxyServer)
	p2p_grpc.RegisterPeerServer(grpcServer, p)

	p.waitGroup.Add(1)
	observability.Go(ctx, func() {
		defer cancelFn()
		defer p.waitGroup.Done()
		logger.Infof(ctx, "started the gRPC server at '%s'", grpcServerListener.Addr())
		err := grpcServer.Serve(grpcServerListener)
		if err != nil {
			logger.Errorf(ctx, "unable to serve the gRPC server: %v", err)
		}
	})
	return nil
}

func (p *peerServer) GetName(context.Context, *p2p_grpc.GetNameRequest) (*p2p_grpc.GetNameReply, error) {
	return &p2p_grpc.GetNameReply{
		Name: p.peer.network.peerName,
	}, nil
}

func (p *peerServer) Ping(ctx context.Context, req *p2p_grpc.PingRequest) (*p2p_grpc.PingReply, error) {
	return &p2p_grpc.PingReply{
		Payload: req.GetPayload(),
	}, nil
}
