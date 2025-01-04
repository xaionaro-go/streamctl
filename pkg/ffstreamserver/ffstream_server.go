package ffstreamserver

import (
	"context"
	"net"

	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	grpc_recovery "github.com/grpc-ecosystem/go-grpc-middleware/recovery"
	"github.com/xaionaro-go/streamctl/pkg/ffstream"
	"github.com/xaionaro-go/streamctl/pkg/ffstreamserver/grpc/go/ffstream_grpc"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"google.golang.org/grpc"
)

type FFStreamServer struct {
	ffStream *ffstream.FFStream
}

func New(ffStream *ffstream.FFStream) *FFStreamServer {
	return &FFStreamServer{
		ffStream: ffStream,
	}
}

func (s *FFStreamServer) ServeContext(
	ctx context.Context,
	listener net.Listener,
) error {
	opts := []grpc_recovery.Option{
		grpc_recovery.WithRecoveryHandler(func(p interface{}) (err error) {
			errmon.ObserveRecoverCtx(ctx, p)
			return nil
		}),
	}
	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			grpc_recovery.UnaryServerInterceptor(opts...),
		),
		grpc.ChainStreamInterceptor(
			grpc_recovery.StreamServerInterceptor(opts...),
		),
	)
	ffstreamGRPC := NewGRPCServer(s.ffStream)
	ffstream_grpc.RegisterFFStreamServer(grpcServer, ffstreamGRPC)

	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()
	observability.Go(ctx, func() {
		<-ctx.Done()
		grpcServer.Stop()
	})
	return grpcServer.Serve(listener)
}
