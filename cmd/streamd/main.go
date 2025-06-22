package main

import (
	"context"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"
	"runtime/pprof"
	"sync"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	errmonsentry "github.com/facebookincubator/go-belt/tool/experimental/errmon/implementation/sentry"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/getsentry/sentry-go"
	grpc_recovery "github.com/grpc-ecosystem/go-grpc-middleware/recovery"
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/grpcproxy/grpcproxyserver"
	"github.com/xaionaro-go/grpcproxy/protobuf/go/proxy_grpc"
	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/cmd/streamd/ui"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streamd/server"
	uiiface "github.com/xaionaro-go/streamctl/pkg/streamd/ui"
	"github.com/xaionaro-go/xpath"
	"google.golang.org/grpc"
)

const forceNetPProfOnAndroid = true

func main() {
	loggerLevel := logger.LevelWarning
	pflag.Var(&loggerLevel, "log-level", "Log level")
	listenAddr := pflag.String(
		"listen-addr",
		":3594",
		"the address to listen for incoming connections to",
	)
	configPath := pflag.String(
		"config-path",
		"/etc/streamd/streamd.yaml",
		"the path to the config file",
	)
	netPprofAddr := pflag.String(
		"go-net-pprof-addr",
		"",
		"address to listen to for net/pprof requests",
	)
	cpuProfile := pflag.String("go-profile-cpu", "", "file to write cpu profile to")
	heapProfile := pflag.String("go-profile-heap", "", "file to write memory profile to")
	sentryDSN := pflag.String("sentry-dsn", "", "DSN of a Sentry instance to send error reports")
	pflag.Parse()

	l := logrus.Default().WithLevel(logger.LevelTrace)
	observability.LogLevelFilter.SetLevel(loggerLevel)
	l = l.WithPreHooks(&observability.LogLevelFilter)

	if *cpuProfile != "" {
		f, err := os.Create(*cpuProfile)
		if err != nil {
			l.Fatalf("unable to create file '%s': %v", *cpuProfile, err)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			l.Fatalf("unable to write to file '%s': %v", *cpuProfile, err)
		}
		defer pprof.StopCPUProfile()
	}

	if *heapProfile != "" {
		f, err := os.Create(*heapProfile)
		if err != nil {
			l.Fatalf("unable to create file '%s': %v", *heapProfile, err)
		}
		defer f.Close()
		runtime.GC()
		if err := pprof.WriteHeapProfile(f); err != nil {
			l.Fatalf("unable to write to file '%s': %v", *heapProfile, err)
		}
	}

	ctx := context.Background()
	ctx = logger.CtxWithLogger(ctx, l)

	if *netPprofAddr != "" || (forceNetPProfOnAndroid && runtime.GOOS == "android") {
		observability.Go(ctx, func(ctx context.Context) {
			if *netPprofAddr == "" {
				*netPprofAddr = "localhost:0"
			}
			l.Infof("starting to listen for net/pprof requests at '%s'", *netPprofAddr)
			l.Error(http.ListenAndServe(*netPprofAddr, nil))
		})
	}

	if oldValue := runtime.GOMAXPROCS(0); oldValue < 16 {
		l.Infof("increased GOMAXPROCS from %d to %d", oldValue, 16)
		runtime.GOMAXPROCS(16)
	}

	if *sentryDSN != "" {
		l.Infof("setting up Sentry at DSN '%s'", *sentryDSN)
		sentryClient, err := sentry.NewClient(sentry.ClientOptions{
			Dsn: *sentryDSN,
		})
		if err != nil {
			l.Fatal(err)
		}
		sentryErrorMonitor := errmonsentry.New(sentryClient)
		ctx = errmon.CtxWithErrorMonitor(ctx, sentryErrorMonitor)

		l = l.WithPreHooks(observability.NewErrorMonitorLoggerHook(
			sentryErrorMonitor,
		))
		ctx = logger.CtxWithLogger(ctx, l)
	}

	logger.Default = func() logger.Logger {
		return l
	}
	defer belt.Flush(ctx)

	configPathExpanded, err := xpath.Expand(*configPath)
	if err != nil {
		l.Fatalf("unable to get the path to the data file: %v", err)
	}

	var wg sync.WaitGroup
	var cancelFunc context.CancelFunc
	var _ui uiiface.UI
	var streamdGRPC *server.GRPCServer
	var obsGRPC obs_grpc.OBSServer
	var grpcLocker sync.Mutex

	restart := func() {
		l.Debugf("restart()")
		if cancelFunc != nil {
			l.Infof("cancelling the old server")
			cancelFunc()
			wg.Wait()
		}

		wg.Add(1)
		defer wg.Done()
		l.Infof("starting a server")
		grpcLocker.Lock()
		defer grpcLocker.Unlock()

		var cfg config.Config
		err := config.ReadConfigFromPath(ctx, configPathExpanded, &cfg)
		if err != nil {
			l.Fatal(err)
		}

		ctx, _cancelFunc := context.WithCancel(ctx)
		cancelFunc = _cancelFunc
		streamD, err := streamd.New(
			cfg,
			_ui,
			func(ctx context.Context, c config.Config) (_err error) {
				logger.Debugf(ctx, "SaveConfig")
				defer func() { logger.Debugf(ctx, "/SaveConfig: %v", _err) }()
				return config.WriteConfigToPath(ctx, configPathExpanded, c)
			},
			belt.CtxBelt(ctx),
		)
		if err != nil {
			l.Fatalf("unable to initialize the streamd instance: %v", err)
		}

		observability.Go(ctx, func(ctx context.Context) {
			if err = streamD.Run(ctx); err != nil {
				l.Errorf("streamd returned an error: %v", err)
			}
		})

		listener, err := net.Listen("tcp", *listenAddr)
		if err != nil {
			log.Fatalf("failed to listen: %v", err)
		}

		observability.Go(ctx, func(ctx context.Context) {
			<-ctx.Done()
			listener.Close()
		})

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
		streamdGRPC = server.NewGRPCServer(streamD)
		var obsGRPCClose context.CancelFunc
		obsGRPC, obsGRPCClose, err = streamD.OBS(ctx)
		if obsGRPCClose != nil {
			defer obsGRPCClose()
		}
		if err != nil {
			log.Fatal(err)
		}
		obs_grpc.RegisterOBSServer(grpcServer, obsGRPC)
		proxy_grpc.RegisterNetworkProxyServer(grpcServer, grpcproxyserver.New())
		streamd_grpc.RegisterStreamDServer(grpcServer, streamdGRPC)
		l.Infof("started server at %s", *listenAddr)

		grpcLocker.Unlock()
		err = grpcServer.Serve(listener)
		grpcLocker.Lock()
		if err != nil {
			log.Fatal(err)
		}
	}

	_ui = ui.NewUI(
		ctx,
		func(ctx context.Context, url string) error {
			grpcLocker.Lock()
			logger.Debugf(ctx, "streamdGRPCLocker.Lock()-ed")
			defer logger.Debugf(ctx, "streamdGRPCLocker.Lock()-ed")
			defer grpcLocker.Unlock()

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

			grpcLocker.Lock()
			logger.Debugf(ctx, "streamdGRPCLocker.Lock()-ed")
			defer logger.Debugf(ctx, "streamdGRPCLocker.Lock()-ed")
			defer grpcLocker.Unlock()

			err := streamdGRPC.OpenOAuthURL(ctx, listenPort, platID, authURL)
			errmon.ObserveErrorCtx(ctx, err)
			return err == nil
		},
		func(ctx context.Context, s string) {
			restart()
		},
		func(ctx context.Context, l logger.Level) {
			observability.LogLevelFilter.SetLevel(l)
		},
	)

	restart()
	<-ctx.Done()
}
