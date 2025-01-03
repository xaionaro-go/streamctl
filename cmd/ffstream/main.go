package main

import (
	"os"

	child_process_manager "github.com/AgustinSRG/go-child-process-manager"
	"github.com/xaionaro-go/streamctl/pkg/encoder/libav/encoder"
	"github.com/xaionaro-go/streamctl/pkg/ffstream"
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

	for _, input := range flags.Inputs {
		input, err := encoder.NewInputFromURL(ctx, input.URL, "", encoder.InputConfig{
			CustomOptions: convertUnknownOptionsToCustomOptions(input.Options),
		})
		assertNoError(err)
		s.AddInput(input)
	}

	output, err := encoder.NewOutputFromURL(ctx, flags.Output.URL, "", encoder.OutputConfig{
		CustomOptions: convertUnknownOptionsToCustomOptions(flags.Output.Options),
	})
	assertNoError(err)
	s.AddOutput(output)

	encoder.NewLoop()
}
