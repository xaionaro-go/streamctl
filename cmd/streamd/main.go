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
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/zap"
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/streamctl/cmd/streamd/ui"
	"github.com/xaionaro-go/streamctl/pkg/streamd"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streamd/server"
	uiiface "github.com/xaionaro-go/streamctl/pkg/streamd/ui"
	"github.com/xaionaro-go/streamctl/pkg/xpath"
	"google.golang.org/grpc"
)

const forceNetPProfOnAndroid = true

func main() {
	loggerLevel := logger.LevelWarning
	pflag.Var(&loggerLevel, "log-level", "Log level")
	listenAddr := pflag.String("listen-addr", ":3594", "the address to listen for incoming connections to")
	configPath := pflag.String("config-path", "/etc/streamd/streamd.yaml", "the path to the config file")
	netPprofAddr := pflag.String("go-net-pprof-addr", "", "address to listen to for net/pprof requests")
	cpuProfile := pflag.String("go-profile-cpu", "", "file to write cpu profile to")
	heapProfile := pflag.String("go-profile-heap", "", "file to write memory profile to")
	pflag.Parse()
	l := zap.Default().WithLevel(loggerLevel)

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

	if *netPprofAddr != "" || (forceNetPProfOnAndroid && runtime.GOOS == "android") {
		go func() {
			if *netPprofAddr == "" {
				*netPprofAddr = "localhost:0"
			}
			l.Infof("starting to listen for net/pprof requests at '%s'", *netPprofAddr)
			l.Error(http.ListenAndServe(*netPprofAddr, nil))
		}()
	}

	if oldValue := runtime.GOMAXPROCS(0); oldValue < 16 {
		l.Infof("increased GOMAXPROCS from %d to %d", oldValue, 16)
		runtime.GOMAXPROCS(16)
	}

	ctx := context.Background()
	ctx = logger.CtxWithLogger(ctx, l)
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
	var streamdGRPCLocker sync.Mutex

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
		streamdGRPCLocker.Lock()
		defer streamdGRPCLocker.Unlock()

		var cfg config.Config
		err := config.ReadConfigFromPath(ctx, configPathExpanded, &cfg)
		if err != nil {
			l.Fatal(cfg)
		}

		ctx, _cancelFunc := context.WithCancel(ctx)
		cancelFunc = _cancelFunc
		streamD, err := streamd.New(
			cfg,
			_ui,
			func(ctx context.Context, c config.Config) error {
				return config.WriteConfigToPath(ctx, configPathExpanded, cfg)
			},
			belt.CtxBelt(ctx),
		)
		if err != nil {
			l.Fatalf("unable to initialize the streamd instance: %v", err)
		}

		if err = streamD.Run(ctx); err != nil {
			l.Fatalf("streamd exited with error: %v", err)
		}

		listener, err := net.Listen("tcp", *listenAddr)
		if err != nil {
			log.Fatalf("failed to listen: %v", err)
		}

		go func() {
			<-ctx.Done()
			listener.Close()
		}()

		grpcServer := grpc.NewServer()
		streamdGRPC = server.NewGRPCServer(streamD)
		streamd_grpc.RegisterStreamDServer(grpcServer, streamdGRPC)
		l.Infof("started server at %s", *listenAddr)
		err = grpcServer.Serve(listener)
		if err != nil {
			log.Fatal(err)
		}
	}

	_ui = ui.NewUI(
		ctx,
		func(authURL string) {
			streamdGRPCLocker.Lock()
			defer streamdGRPCLocker.Unlock()
			streamdGRPC.OpenOAuthURL(authURL)
		},
		func(ctx context.Context, s string) {
			restart()
		},
	)

	restart()
	<-ctx.Done()
}
