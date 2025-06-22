package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"
	"runtime/pprof"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/xaionaro-go/observability"
)

func initRuntime(
	ctx context.Context,
	flags Flags,
	_procName ProcessName,
) (context.Context, context.CancelFunc) {
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
			http.Handle(
				"/metrics",
				promhttp.Handler(),
			) // TODO: either split this from pprof argument, or rename the argument (and re-describe it)

			l.Infof("starting to listen for net/pprof requests at '%s'", netPprofAddr)
			l.Error(http.ListenAndServe(netPprofAddr, nil))
		})
	}

	if oldValue := runtime.GOMAXPROCS(0); oldValue < 4 {
		l.Infof("increased GOMAXPROCS from %d to %d", oldValue, 4)
		runtime.GOMAXPROCS(4)
	}

	observability.Go(ctx, func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			<-t.C
			belt.Flush(ctx)
		}
	})

	seppukuIfMemHugeLeak(ctx)

	ctx, cancelFn := context.WithCancel(ctx)
	return ctx, func() {
		defer belt.Flush(ctx)
		cancelFn()
		for i := len(closeFuncs) - 1; i >= 0; i-- {
			closeFuncs[i]()
		}
	}
}

var seppukuIfMemHugeLeakCanceler context.CancelFunc

func seppukuIfMemHugeLeak(
	ctx context.Context,
) {
	if seppukuIfMemHugeLeakCanceler != nil {
		seppukuIfMemHugeLeakCanceler()
	}

	ctx, cancelFn := context.WithCancel(ctx)
	seppukuIfMemHugeLeakCanceler = cancelFn

	go func() {
		var buf bytes.Buffer
		for {
			t := time.NewTicker(time.Second)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}

				var m runtime.MemStats
				runtime.ReadMemStats(&m)

				logger.Debugf(
					ctx,
					"memory consumed (in heap): %v (%v)",
					humanize.Bytes(m.HeapInuse),
					m.HeapInuse,
				)
				if m.HeapInuse > 1000*1000*1000 && logger.FromCtx(ctx).Level() >= logger.LevelDebug {
					buf.Reset()
					err := pprof.WriteHeapProfile(&buf)
					if err == nil {
						fmt.Fprintf(os.Stderr, "HEAP-PROFILE: %s\n", base64.StdEncoding.EncodeToString(buf.Bytes()))
					} else {
						logger.Errorf(ctx, "unable to get heap profile: %v", err)
					}
				}
				if m.HeapInuse > 4*1000*1000*1000 {
					logger.Panicf(ctx, "I consumed almost 4GiB! Seppuku!")
				}
			}
		}
	}()
}
