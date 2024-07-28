package main

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
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

func init() {
	gob.Register(RequestStreamDConfig{})
	gob.Register(mainprocess.MessageReady{
		ReadyForMessages: []any{GetStreamdAddress{}, RequestStreamDConfig{}},
	})
}

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

	runStreamd(ctx, flags, mainProcess)
}

func runStreamd(
	ctx context.Context,
	flags Flags,
	mainProcess *mainprocess.Client,
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
	streamdGRPCLocker.Lock()

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
			var buf bytes.Buffer
			_, err := cfg.WriteTo(&buf)
			if err != nil {
				return fmt.Errorf("unable to serialize the config: %w", err)
			}
			if mainProcess != nil {
				return mainProcess.SendMessage(ctx, ProcessNameUI, UpdateStreamDConfig{
					Config: buf.String(),
				})
			}
			panic("not implemented")
		},
		belt.CtxBelt(ctx),
	)
	if err != nil {
		logger.Fatalf(ctx, "unable to initialize streamd: %v", err)
	}

	var listener net.Listener
	listener, _, streamdGRPC = initGRPCServer(ctx, streamD, flags.ListenAddr)
	streamdGRPCLocker.Unlock()

	var configLocker sync.Mutex
	configLocker.Lock()
	if mainProcess != nil {
		logger.Debugf(ctx, "starting the IPC server")
		setReadyFor(ctx, mainProcess, GetStreamdAddress{}, RequestStreamDConfig{})
		go func() {
			err := mainProcess.Serve(
				ctx,
				func(
					ctx context.Context,
					source mainprocess.ProcessName,
					content any,
				) error {
					switch content.(type) {
					case GetStreamdAddress:
						return mainProcess.SendMessage(ctx, source, GetStreamdAddressResult{
							Address: listener.Addr().String(),
						})
					case RequestStreamDConfig:
						var buf bytes.Buffer
						configLocker.Lock()
						_, err := cfg.BuiltinStreamD.WriteTo(&buf)
						configLocker.Unlock()
						if err != nil {
							return fmt.Errorf("unable to serialize the config: %w", err)
						}
						return mainProcess.SendMessage(ctx, source, UpdateStreamDConfig{
							Config: buf.String(),
						})
					default:
						return fmt.Errorf("unexpected message of type %T: %#+v", content, content)
					}
				},
			)
			logger.Fatalf(ctx, "communication (with the main process) error: %v", err)
		}()
	}

	err = streamD.Run(ctx)
	if err != nil {
		logger.Fatalf(ctx, "unable to start streamd: %v", err)
	}
	configLocker.Unlock()

	logger.Infof(ctx, "streamd is ready")
	<-ctx.Done()

	logger.Fatalf(ctx, "internal error: was supposed to never reach this line")
}

type RequestStreamDConfig struct{}
type UpdateStreamDConfig struct {
	Config string
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
