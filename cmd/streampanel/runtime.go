package main

import (
	"bytes"
	"context"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"
	"runtime/pprof"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/observability"
)

func initRuntime(ctx context.Context, flags Flags, _procName ProcessName) context.CancelFunc {
	procName := string(_procName)
	var closeFuncs []func()

	l := logger.FromCtx(ctx)

	if ForceDebug {
		observability.Go(ctx, func() {
			t := time.NewTicker(time.Second)
			defer t.Stop()
			for {
				var buf bytes.Buffer
				err := pprof.Lookup("goroutine").WriteTo(&buf, 1)
				if err != nil {
					l.Error(err)
					continue
				}
				l.Tracef("stacktraces:\n%s", buf.String())
				<-t.C
			}
		})
	}

	if flags.CPUProfile != "" {
		f, err := os.Create(flags.CPUProfile + "-" + procName)
		if err != nil {
			l.Fatalf("unable to create file '%s': %v", flags.CPUProfile+"-"+procName, err)
		}
		closeFuncs = append(closeFuncs, func() { f.Close() })
		if err := pprof.StartCPUProfile(f); err != nil {
			l.Fatalf("unable to write to file '%s': %v", flags.CPUProfile+"-"+procName, err)
		}
		closeFuncs = append(closeFuncs, pprof.StopCPUProfile)
	}

	if flags.HeapProfile != "" {
		f, err := os.Create(flags.HeapProfile + "-" + procName)
		if err != nil {
			l.Fatalf("unable to create file '%s': %v", flags.HeapProfile+"-"+procName, err)
		}
		closeFuncs = append(closeFuncs, func() { f.Close() })
		runtime.GC()
		if err := pprof.WriteHeapProfile(f); err != nil {
			l.Fatalf("unable to write to file '%s': %v", flags.HeapProfile+"-"+procName, err)
		}
	}

	netPprofAddr := ""
	switch _procName {
	case ProcessNameMain:
		netPprofAddr = flags.NetPprofAddrMain
	case ProcessNameUI:
		netPprofAddr = flags.NetPprofAddrUI
	case ProcessNameStreamd:
		netPprofAddr = flags.NetPprofAddrStreamD
	}
	if netPprofAddr == "" && forceNetPProfOnAndroid && runtime.GOOS == "android" {
		if ForceDebug {
			netPprofAddr = "0.0.0.0:0"
		} else {
			netPprofAddr = "localhost:0"
		}
	}

	if netPprofAddr != "" {
		observability.Go(ctx, func() {
			l.Infof("starting to listen for net/pprof requests at '%s'", netPprofAddr)
			l.Error(http.ListenAndServe(netPprofAddr, nil))
		})
	}

	if oldValue := runtime.GOMAXPROCS(0); oldValue < 16 {
		l.Infof("increased GOMAXPROCS from %d to %d", oldValue, 16)
		runtime.GOMAXPROCS(16)
	}

	return func() {
		for i := len(closeFuncs) - 1; i >= 0; i-- {
			closeFuncs[i]()
		}
	}
}
