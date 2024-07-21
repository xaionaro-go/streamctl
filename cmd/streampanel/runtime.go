package main

import (
	"context"
	"net/http"
	"os"
	"runtime"
	"runtime/pprof"

	"github.com/facebookincubator/go-belt/tool/logger"
)

func initRuntime(ctx context.Context, flags Flags, _procName ProcessName) context.CancelFunc {
	procName := string(_procName)
	var closeFuncs []func()

	l := logger.FromCtx(ctx)

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
	if forceNetPProfOnAndroid && runtime.GOOS == "android" {
		netPprofAddr = "localhost:0"
	}
	switch _procName {
	case ProcessNameMain:
		netPprofAddr = flags.NetPprofAddrMain
	case ProcessNameUI:
		netPprofAddr = flags.NetPprofAddrUI
	case ProcessNameStreamd:
		netPprofAddr = flags.NetPprofAddrStreamD
	}

	if netPprofAddr != "" {
		go func() {
			l.Infof("starting to listen for net/pprof requests at '%s'", netPprofAddr)
			l.Error(http.ListenAndServe(netPprofAddr, nil))
		}()
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
