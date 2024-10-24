package main

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	grpc_recovery "github.com/grpc-ecosystem/go-grpc-middleware/recovery"
	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
	"github.com/xaionaro-go/streamctl/cmd/streamd/ui"
	"github.com/xaionaro-go/streamctl/pkg/mainprocess"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streamd/server"
	streampanelconfig "github.com/xaionaro-go/streamctl/pkg/streampanel/config"
	"github.com/xaionaro-go/streamctl/pkg/xpath"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
	"google.golang.org/grpc"
)

func init() {
	gob.Register(RequestStreamDConfig{})
	gob.Register(mainprocess.MessageReady{
		ReadyForMessages: []any{GetStreamdAddress{}, RequestStreamDConfig{}},
	})
}

func forkStreamd(preCtx context.Context, mainProcessAddr, password string) {
	procName := ProcessNameStreamd

	mainProcess, err := mainprocess.NewClient(
		procName,
		mainProcessAddr,
		password,
	)
	if err != nil {
		panic(err)
	}
	flags := getFlags(preCtx, mainProcess)
	ctx := getContext(flags)
	ctx = belt.WithField(ctx, "process", procName)
	logger.Debugf(ctx, "flags == %#+v", flags)
	ctx, cancelFunc := initRuntime(ctx, flags, procName)
	defer cancelFunc()
	observability.Go(ctx, func() {
		<-ctx.Done()
		logger.Debugf(ctx, "context is cancelled")
	})

	defer func() { observability.PanicIfNotNil(ctx, recover()) }()

	childProcessSignalHandler(ctx, cancelFunc)

	runStreamd(ctx, cancelFunc, flags, mainProcess)
}

func runStreamd(
	ctx context.Context,
	cancelFunc context.CancelFunc,
	flags Flags,
	mainProcess *mainprocess.Client,
) {
	logger.Debugf(ctx, "runStreamd: %#+v", flags)
	defer logger.Debugf(ctx, "/runStreamd")
	if flags.RemoteAddr != "" {
		logger.Panic(ctx, "not implemented")
	}

	ctx = belt.WithField(ctx, "streamd_addr", flags.RemoteAddr)
	l := logger.FromCtx(ctx)
	logger.Default = func() logger.Logger {
		return l
	}

	configPath, err := xpath.Expand(flags.ConfigPath)
	if err != nil {
		logger.Panic(ctx, err)
	}

	var cfg streampanelconfig.Config
	err = streampanelconfig.ReadConfigFromPath(configPath, &cfg)
	if err != nil {
		logger.Panicf(ctx, "unable to read the config from path '%s': %v", flags.ConfigPath, err)
	}

	var streamdGRPCLocker sync.Mutex
	streamdGRPCLocker.Lock()

	var streamdGRPC *server.GRPCServer
	ui := ui.NewUI(
		ctx,
		func(ctx context.Context, url string) error {
			streamdGRPCLocker.Lock()
			logger.Debugf(ctx, "streamdGRPCLocker.Lock()-ed")
			defer logger.Debugf(ctx, "streamdGRPCLocker.Lock()-ed")
			defer streamdGRPCLocker.Unlock()

			err := streamdGRPC.OpenBrowser(ctx, url)
			errmon.ObserveErrorCtx(ctx, err)
			return err
		},
		func(listenPort uint16, platID streamcontrol.PlatformName, authURL string) bool {
			logger.Debugf(ctx, "streamd.UI.OpenOAuthURL(%d, %s, '%s')", listenPort, platID, authURL)
			defer logger.Debugf(
				ctx,
				"/streamd.UI.OpenOAuthURL(%d, %s, '%s')",
				listenPort,
				platID,
				authURL,
			)

			streamdGRPCLocker.Lock()
			logger.Debugf(ctx, "streamdGRPCLocker.Lock()-ed")
			defer logger.Debugf(ctx, "streamdGRPCLocker.Lock()-ed")
			defer streamdGRPCLocker.Unlock()

			err := streamdGRPC.OpenOAuthURL(ctx, listenPort, platID, authURL)
			errmon.ObserveErrorCtx(ctx, err)
			return err == nil
		},
		func(ctx context.Context, s string) {
			logger.Infof(ctx, "restarting streamd")
			cancelFunc()
			os.Exit(0)
		},
		func(ctx context.Context, l logger.Level) {
			observability.LogLevelFilter.SetLevel(l)
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
		logger.Panicf(ctx, "unable to initialize streamd: %v", err)
	}

	var listener net.Listener
	listener, _, streamdGRPC, _ = initGRPCServers(ctx, streamD, flags.ListenAddr)
	streamdGRPCLocker.Unlock()

	var configLocker xsync.Mutex
	configLocker.Do(ctx, func() {
		if mainProcess != nil {
			logger.Debugf(ctx, "starting the IPC server")
			setReadyFor(ctx, mainProcess, GetStreamdAddress{}, RequestStreamDConfig{})
			observability.Go(ctx, func() {
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
							err := xsync.DoR1(ctx, &configLocker, func() error {
								_, err := cfg.BuiltinStreamD.WriteTo(&buf)
								return err
							})
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
				select {
				case <-ctx.Done():
					return
				case <-time.After(10 * time.Millisecond): // TODO: here should be `default:` instead of this ugly racy hack
				}
				logger.Panicf(ctx, "communication (with the main process) error: %v", err)
			})
		}

		err = streamD.Run(ctx)
		if err != nil {
			logger.Errorf(ctx, "unable to start streamd: %v", err)
			logger.Panicf(ctx, "unable to start streamd: %v", err)
		}
	})

	logger.Infof(ctx, "streamd is ready")
	<-ctx.Done()
	time.Sleep(5 * time.Second)

	logger.Panicf(ctx, "internal error: was supposed to never reach this line")
}

type RequestStreamDConfig struct{}
type UpdateStreamDConfig struct {
	Config string
}

func initGRPCServers(
	ctx context.Context,
	streamD api.StreamD,
	listenAddr string,
) (net.Listener, *grpc.Server, *server.GRPCServer, obs_grpc.OBSServer) {
	logger.Debugf(ctx, "initGRPCServers")
	defer logger.Debugf(ctx, "/initGRPCServers")
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		logger.Panicf(ctx, "failed to listen: %v", err)
	}

	if streamD == nil {
		logger.Panicf(ctx, "streamD is nil")
	}

	obsGRPC, obsGRPCClose, err := streamD.OBS(ctx)
	observability.Go(ctx, func() {
		<-ctx.Done()
		listener.Close()
		if obsGRPCClose != nil {
			obsGRPCClose()
		}
	})
	if err != nil {
		logger.Panicf(ctx, "unable to initialize OBS client: %v", err)
	}

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
	streamdGRPC := server.NewGRPCServer(streamD)
	streamd_grpc.RegisterStreamDServer(grpcServer, streamdGRPC)
	obs_grpc.RegisterOBSServer(grpcServer, obsGRPC)

	// start the server:
	observability.Go(ctx, func() {
		logger.Infof(ctx, "started server at %s", listener.Addr().String())
		err = grpcServer.Serve(listener)
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Millisecond): // TODO: remove this hack
		}
		if err != nil {
			logger.Panic(ctx, err)
		}
	})

	return listener, grpcServer, streamdGRPC, obsGRPC
}
