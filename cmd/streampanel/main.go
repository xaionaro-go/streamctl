package main

import (
	"context"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"
	"runtime/pprof"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	errmonsentry "github.com/facebookincubator/go-belt/tool/experimental/errmon/implementation/sentry"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/getsentry/sentry-go"
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streamd/server"
	"github.com/xaionaro-go/streamctl/pkg/streampanel"
	"google.golang.org/grpc"
)

const forceNetPProfOnAndroid = true

func main() {
	loggerLevel := logger.LevelWarning
	pflag.Var(&loggerLevel, "log-level", "Log level")
	listenAddr := pflag.String("listen-addr", "", "the address to listen for incoming connections to")
	remoteAddr := pflag.String("remote-addr", "", "the address (for example 127.0.0.1:3594) of streamd to connect to, instead of running the stream controllers locally")
	configPath := pflag.String("config-path", "~/.streampanel.yaml", "the path to the config file")
	netPprofAddr := pflag.String("go-net-pprof-addr", "", "address to listen to for net/pprof requests")
	cpuProfile := pflag.String("go-profile-cpu", "", "file to write cpu profile to")
	heapProfile := pflag.String("go-profile-heap", "", "file to write memory profile to")
	sentryDSN := pflag.String("sentry-dsn", "", "DSN of a Sentry instance to send error reports")
	pflag.Parse()
	l := logrus.Default().WithLevel(loggerLevel)

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

	listener, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		l.Fatalf("failed to listen: %v", err)
	}

	ctx := context.Background()
	go func() {
		<-ctx.Done()
		listener.Close()
	}()

	var opts []streampanel.Option
	if *remoteAddr != "" {
		opts = append(opts, streampanel.OptionRemoteStreamDAddr(*remoteAddr))
	}
	if *sentryDSN != "" {
		opts = append(opts, streampanel.OptionSentryDSN(*sentryDSN))
	}
	panel, panelErr := streampanel.New(*configPath, opts...)

	if panel.Config.SentryDSN != "" {
		l.Infof("setting up Sentry at DSN '%s'", panel.Config.SentryDSN)
		sentryClient, err := sentry.NewClient(sentry.ClientOptions{
			Dsn: panel.Config.SentryDSN,
		})
		if err != nil {
			l.Fatal(err)
		}
		sentryErrorMonitor := errmonsentry.New(sentryClient)
		ctx = errmon.CtxWithErrorMonitor(ctx, sentryErrorMonitor)
		l = l.WithPreHooks(observability.NewErrorMonitorLoggerHook(
			sentryErrorMonitor,
		))
	}

	ctx = logger.CtxWithLogger(ctx, l)
	logger.Default = func() logger.Logger {
		return l
	}
	defer belt.Flush(ctx)

	if panelErr != nil {
		l.Fatal(panelErr)
	}

	if *listenAddr != "" {
		go func() {
			grpcServer := grpc.NewServer()
			streamdGRPC := server.NewGRPCServer(panel.StreamD)
			streamd_grpc.RegisterStreamDServer(grpcServer, streamdGRPC)
			l.Infof("started server at %s", *listenAddr)
			err = grpcServer.Serve(listener)
			if err != nil {
				l.Fatal(err)
			}
		}()
	}

	err = panel.Loop(ctx)
	if err != nil {
		l.Fatal(err)
	}
}
