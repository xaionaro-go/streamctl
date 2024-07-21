package main

import (
	"context"
	"net"
	"os"
	"sync"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/cmd/streamd/ui"
	"github.com/xaionaro-go/streamctl/pkg/mainprocess"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streamd/server"
	streampanelconfig "github.com/xaionaro-go/streamctl/pkg/streampanel/config"
	"github.com/xaionaro-go/streamctl/pkg/xpath"
	"google.golang.org/grpc"
)

func forkStreamd(ctx context.Context, mainProcessAddr, password string) {
	procName := ProcessNameStreamd
	ctx = belt.WithField(ctx, "process", procName)

	mainProcess, err := mainprocess.NewClient(
		procName,
		mainProcessAddr,
		password,
	)
	if err != nil {
		panic(err)
	}
	flags := getFlags(ctx, mainProcess)
	ctx = getContext(flags)
	ctx = belt.WithField(ctx, "process", procName)
	defer belt.Flush(ctx)
	logger.Debugf(ctx, "flags == %#+v", flags)
	cancelFunc := initRuntime(ctx, flags, procName)
	defer cancelFunc()

	runStreamd(ctx, flags)
}

func runStreamd(
	ctx context.Context,
	flags Flags,
) {
	logger.Debugf(ctx, "runStreamd: %#+v", flags)
	defer logger.Debugf(ctx, "/runStreamd")
	if flags.RemoteAddr != "" {
		logger.Fatal(ctx, "not implemented")
	}

	configPath, err := xpath.Expand(flags.ConfigPath)
	if err != nil {
		logger.Fatal(ctx, err)
	}

	var cfg streampanelconfig.Config
	err = streampanelconfig.ReadConfigFromPath(configPath, &cfg)
	if err != nil {
		logger.Fatalf(ctx, "unable to read the config from path '%s': %v", flags.ConfigPath, err)
	}

	var streamdGRPCLocker sync.Mutex

	var streamdGRPC *server.GRPCServer
	ui := ui.NewUI(
		ctx,
		func(listenPort uint16, platID streamcontrol.PlatformName, authURL string) bool {
			logger.Tracef(ctx, "streamd.UI.OpenOAuthURL(%d, %s, '%s')", listenPort, platID, authURL)
			defer logger.Tracef(ctx, "/streamd.UI.OpenOAuthURL(%d, %s, '%s')", listenPort, platID, authURL)

			streamdGRPCLocker.Lock()
			logger.Tracef(ctx, "streamdGRPCLocker.Lock()-ed")
			defer logger.Tracef(ctx, "streamdGRPCLocker.Lock()-ed")
			defer streamdGRPCLocker.Unlock()

			err := streamdGRPC.OpenOAuthURL(ctx, listenPort, platID, authURL)
			errmon.ObserveErrorCtx(ctx, err)
			return err == nil
		},
		func(ctx context.Context, s string) {
			logger.Infof(ctx, "restarting streamd")
			os.Exit(0)
		},
	)

	streamD, err := streamd.New(
		cfg.BuiltinStreamD,
		ui,
		func(ctx context.Context, cfg config.Config) error {
			return nil
		},
		belt.CtxBelt(ctx),
	)
	if err != nil {
		logger.Fatalf(ctx, "unable to initialize streamd: %v", err)
	}

	var listener net.Listener
	var grpcServer *grpc.Server
	listener, grpcServer, streamdGRPC = initGRPCServer(ctx, streamD, flags.ListenAddr)

	err = streamD.Run(ctx)
	if err != nil {
		logger.Fatalf(ctx, "unable to start streamd: %v", err)
	}

	err = grpcServer.Serve(listener)
	if err != nil {
		logger.Fatalf(ctx, "unable to server the gRPC server: %v", err)
	}

	logger.Fatalf(ctx, "internal error: was supposed to never reach this line")
}

func initGRPCServer(
	ctx context.Context,
	streamD api.StreamD,
	listenAddr string,
) (net.Listener, *grpc.Server, *server.GRPCServer) {
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		logger.Fatalf(ctx, "failed to listen: %v", err)
	}
	go func() {
		<-ctx.Done()
		listener.Close()
	}()

	grpcServer := grpc.NewServer()
	streamdGRPC := server.NewGRPCServer(streamD)
	streamd_grpc.RegisterStreamDServer(grpcServer, streamdGRPC)

	// start the server:
	go func() {
		logger.Infof(ctx, "started server at %s", listener.Addr().String())
		err = grpcServer.Serve(listener)
		if err != nil {
			logger.Fatal(ctx, err)
		}
	}()

	return listener, grpcServer, streamdGRPC
}