package streampanel

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/grpcproxy/grpcproxyserver"
	"github.com/xaionaro-go/grpcproxy/protobuf/go/proxy_grpc"
	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamd"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/client"
	streamdconfig "github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streamd/server"
	"google.golang.org/grpc"
)

func (p *Panel) LazyInitStreamD(ctx context.Context) (_err error) {
	if p.StreamD != nil {
		return nil
	}
	logger.Debugf(ctx, "initializing StreamD")
	defer func() {
		if p.StreamD == nil {
			_err = fmt.Errorf("somehow we initialized StreamD, but it is still nil")
		}
		logger.Debugf(ctx, "/initializing StreamD: %v", _err)
	}()

	if p.Config.RemoteStreamDAddr != "" {
		if err := p.initRemoteStreamD(ctx); err != nil {
			return fmt.Errorf(
				"unable to initialize the remote stream controller '%s': %w",
				p.Config.RemoteStreamDAddr,
				err,
			)
		}
	} else {
		if err := p.initBuiltinStreamD(ctx); err != nil {
			return fmt.Errorf("unable to initialize the builtin stream controller '%s': %w", p.configPath, err)
		}
	}
	return nil
}

func (p *Panel) streamDCallWrapper(
	ctx context.Context,
	req any,
	callFunc func(ctx context.Context, opts ...grpc.CallOption) error,
	opts ...grpc.CallOption,
) error {
	windowCtx, cancelFunc := context.WithCancel(ctx)
	defer cancelFunc()
	p.showWaitStreamDCallWindow(windowCtx)
	return callFunc(ctx, opts...)
}

func (p *Panel) streamDConnectWrapper(
	ctx context.Context,
	connectFunc func(ctx context.Context, opts ...grpc.DialOption) (*grpc.ClientConn, error),
	opts ...grpc.DialOption,
) (*grpc.ClientConn, error) {
	windowCtx, cancelFunc := context.WithCancel(ctx)
	defer cancelFunc()
	p.showWaitStreamDConnectWindow(windowCtx)
	return connectFunc(ctx, opts...)
}

func (p *Panel) initRemoteStreamD(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "initRemoteStreamD")
	defer func() { logger.Debugf(ctx, "/initRemoteStreamD: %v", _err) }()
	var err error
	p.StreamD, err = client.New(
		ctx,
		p.Config.RemoteStreamDAddr,
		client.OptionConnectWrapper(p.streamDConnectWrapper),
		client.OptionCallWrapper(p.streamDCallWrapper),
	)
	return err
}

func (p *Panel) initBuiltinStreamD(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "initBuiltinStreamD")
	defer func() { logger.Debugf(ctx, "/initBuiltinStreamD: %v", _err) }()

	var (
		streamdGRPC *server.GRPCServer
		obsGRPC     obs_grpc.OBSServer
		proxyGRPC   proxy_grpc.NetworkProxyServer
	)

	var err error
	p.StreamD, err = streamd.New(
		p.Config.BuiltinStreamD,
		p,
		func(ctx context.Context, cfg streamdconfig.Config) error {
			p.Config.BuiltinStreamD = cfg
			return p.SaveConfig(ctx)
		},
		belt.CtxBelt(ctx),
		streamd.OptionP2PSetupServer(func(grpcServer *grpc.Server) error {
			registerGRPCServices(grpcServer, streamdGRPC, obsGRPC, proxyGRPC)
			return nil
		}),
		streamd.OptionP2PSetupClient(func(clientConn *grpc.ClientConn) error {
			return nil
		}),
	)
	if err != nil {
		return fmt.Errorf("unable to initialize the streamd instance: %w", err)
	}

	streamdGRPC, obsGRPC, proxyGRPC, err = initGRPCServers(ctx, p.StreamD)
	if err != nil {
		return fmt.Errorf("unable to initialize gRPC servers: %w", err)
	}

	return nil
}

func initGRPCServers(
	ctx context.Context,
	streamD api.StreamD,
) (*server.GRPCServer, obs_grpc.OBSServer, proxy_grpc.NetworkProxyServer, error) {
	logger.Debugf(ctx, "initGRPCServers")
	defer logger.Debugf(ctx, "/initGRPCServers")

	if streamD == nil {
		return nil, nil, nil, fmt.Errorf("streamD is nil")
	}

	obsGRPC, obsGRPCClose, err := streamD.OBS(ctx)
	observability.Go(ctx, func() {
		<-ctx.Done()
		if obsGRPCClose != nil {
			obsGRPCClose()
		}
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("unable to initialize OBS client: %w", err)
	}

	streamdGRPC := server.NewGRPCServer(streamD)
	proxyGRPC := grpcproxyserver.New()
	return streamdGRPC, obsGRPC, proxyGRPC, nil
}

func registerGRPCServices(
	grpcServer *grpc.Server,
	streamdGRPC *server.GRPCServer,
	obsGRPC obs_grpc.OBSServer,
	proxyGRPC proxy_grpc.NetworkProxyServer,
) {
	streamd_grpc.RegisterStreamDServer(grpcServer, streamdGRPC)
	obs_grpc.RegisterOBSServer(grpcServer, obsGRPC)
	proxy_grpc.RegisterNetworkProxyServer(grpcServer, proxyGRPC)
}
