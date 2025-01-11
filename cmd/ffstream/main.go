package main

import (
	"os"

	child_process_manager "github.com/AgustinSRG/go-child-process-manager"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/ffstream"
	"github.com/xaionaro-go/streamctl/pkg/ffstreamserver"
	"github.com/xaionaro-go/streamctl/pkg/recoder/libav/recoder"
	_ "github.com/xaionaro-go/streamctl/pkg/streamserver"
)

func main() {
	err := child_process_manager.InitializeChildProcessManager()
	if err != nil {
		panic(err)
	}
	defer child_process_manager.DisposeChildProcessManager()

	flags := parseFlags(os.Args)
	ctx := getContext(flags)

	ctx, cancelFunc := initRuntime(ctx, flags)
	defer cancelFunc()

	s := ffstream.New()

	if flags.ListenControlSocket != "" {
		logger.Debugf(ctx, "flags.ListenControlSocket == '%s'", flags.ListenControlSocket)
		listener, err := getListener(ctx, flags.ListenControlSocket)
		assertNoError(ctx, err)

		observability.Go(ctx, func() {
			logger.Infof(ctx, "listening for gRPC clients at %s (%T)", listener.Addr(), listener)
			ffstreamserver.New(s).ServeContext(ctx, listener)
		})
	}

	for _, input := range flags.Inputs {
		input, err := recoder.NewInputFromURL(ctx, input.URL, "", recoder.InputConfig{
			CustomOptions: convertUnknownOptionsToCustomOptions(input.Options),
		})
		assertNoError(ctx, err)
		s.AddInput(ctx, input)
	}

	output, err := recoder.NewOutputFromURL(ctx, flags.Output.URL, "", recoder.OutputConfig{
		CustomOptions: convertUnknownOptionsToCustomOptions(flags.Output.Options),
	})
	assertNoError(ctx, err)
	s.AddOutput(ctx, output)

	err = s.SetEncoderConfig(ctx, ffstream.EncoderConfig{
		Audio: ffstream.CodecConfig{
			CodecName:     flags.AudioEncoder.Codec,
			CustomOptions: convertUnknownOptionsToCustomOptions(flags.AudioEncoder.Options),
		},
		Video: ffstream.CodecConfig{
			CodecName:     flags.VideoEncoder.Codec,
			CustomOptions: convertUnknownOptionsToCustomOptions(flags.VideoEncoder.Options),
		},
	})
	assertNoError(ctx, err)

	err = s.Start(ctx)
	assertNoError(ctx, err)

	err = s.Wait(ctx)
	assertNoError(ctx, err)
}
