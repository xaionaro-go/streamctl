package main

import (
	"context"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/streamctl/pkg/audio/resampler"
	"github.com/xaionaro-go/streamctl/pkg/audio/types"
	"github.com/xaionaro-go/streamctl/pkg/observability"
)

func syntaxExit(message string) {
	fmt.Fprintf(os.Stderr, "syntax error: %s\n", message)
	pflag.Usage()
	os.Exit(2)
}

func main() {
	loggerLevel := logger.LevelDebug
	pflag.Var(&loggerLevel, "log-level", "Log level")
	netPprofAddr := pflag.String("net-pprof-listen-addr", "", "an address to listen for incoming net/pprof connections")
	srcFormatFlag := pflag.String("src-format", "F32LE", "")
	srcSampleRate := pflag.Uint("src-rate", 48000, "")
	srcChannels := pflag.Uint("src-channels", 2, "")
	dstFormatFlag := pflag.String("dst-format", "S16LE", "")
	dstSampleRate := pflag.Uint("dst-rate", 16000, "")
	dstChannels := pflag.Uint("dst-channels", 1, "")
	pflag.Parse()
	if pflag.NArg() != 0 {
		syntaxExit("expected zero arguments")
	}

	l := logrus.Default().WithLevel(loggerLevel)
	ctx := logger.CtxWithLogger(context.Background(), l)
	logger.Default = func() logger.Logger {
		return l
	}
	defer belt.Flush(ctx)

	if *netPprofAddr != "" {
		observability.Go(ctx, func() { l.Error(http.ListenAndServe(*netPprofAddr, nil)) })
	}

	srcFormat := types.PCMFormatFromString(*srcFormatFlag)
	if srcFormat == types.PCMFormatUndefined {
		panic(fmt.Errorf("unknown PCM format '%s'", *srcFormatFlag))
	}

	dstFormat := types.PCMFormatFromString(*dstFormatFlag)
	if srcFormat == types.PCMFormatUndefined {
		panic(fmt.Errorf("unknown PCM format '%s'", *dstFormatFlag))
	}

	formatSrc := resampler.Format{
		Channels:   types.Channel(*srcChannels),
		SampleRate: types.SampleRate(*srcSampleRate),
		PCMFormat:  srcFormat,
	}

	formatDst := resampler.Format{
		Channels:   types.Channel(*dstChannels),
		SampleRate: types.SampleRate(*dstSampleRate),
		PCMFormat:  dstFormat,
	}

	resampler, err := resampler.NewResampler(formatSrc, os.Stdin, formatDst)
	if err != nil {
		panic(err)
	}

	buf := make([]byte, 1024*1024)
	for {
		n, err := resampler.Read(buf)
		if err != nil {
			panic(err)
		}
		w, err := os.Stdout.Write(buf[:n])
		if err != nil {
			panic(err)
		}
		if w != n {
			panic(fmt.Errorf("the written a message not equal to the expected: %d != %d", w, n))
		}
	}
}
