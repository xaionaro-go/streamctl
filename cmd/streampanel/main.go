package main

import (
	"context"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"
	"runtime/pprof"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/zap"
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/streamctl/pkg/streampanel"
)

func main() {
	loggerLevel := logger.LevelWarning
	pflag.Var(&loggerLevel, "log-level", "Log level")
	configPath := pflag.String("config-path", "~/.streampanel.yaml", "the path to the config file")
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

	if *netPprofAddr != "" {
		go func() {
			l.Infof("starting to listen for net/pprof requests at '%s'", *netPprofAddr)
			l.Error(http.ListenAndServe(*netPprofAddr, nil))
		}()
	}

	ctx := context.Background()
	ctx = logger.CtxWithLogger(ctx, l)
	logger.Default = func() logger.Logger {
		return l
	}
	defer belt.Flush(ctx)

	err := streampanel.New(*configPath).Loop(ctx)
	if err != nil {
		l.Fatal(err)
	}
}
