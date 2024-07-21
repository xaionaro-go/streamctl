package main

import (
	"context"
	"fmt"
	"os"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/mainprocess"
)

func forkUI(ctx context.Context, mainProcessAddr, password string) {
	procName := ProcessNameUI
	ctx = belt.WithField(ctx, "process", procName)

	mainProcess, err := mainprocess.NewClient(
		procName,
		mainProcessAddr,
		password,
	)
	if err != nil {
		panic(err)
	}
	flags := getFlags(ctx, mainProcess)
	ctx = getContext(flags)
	ctx = belt.WithField(ctx, "process", procName)
	defer belt.Flush(ctx)
	logger.Debugf(ctx, "flags == %#+v", flags)
	cancelFunc := initRuntime(ctx, flags, procName)
	defer cancelFunc()

	streamdAddr := getStreamDAddress(ctx, mainProcess)
	go mainProcess.Serve(
		ctx,
		func(ctx context.Context, source mainprocess.ProcessName, content any) error {
			switch content.(type) {
			case StreamDDied:
				os.Exit(0)
			}
			return nil
		},
	)

	flags.RemoteAddr = streamdAddr
	runPanel(ctx, flags)
}

type StreamDDied struct{}

type GetStreamdAddress struct{}
type GetStreamdAddressResult struct {
	Address string
}

func getStreamDAddress(
	ctx context.Context,
	mainProcess *mainprocess.Client,
) string {
	err := mainProcess.SendMessage(ctx, "streamd", GetStreamdAddress{})
	assertNoError(err)

	var addr string
	for {
		readOnceMore := false
		err = mainProcess.ReadOne(
			ctx,
			func(ctx context.Context, source mainprocess.ProcessName, content any) error {
				switch msg := content.(type) {
				case GetStreamdAddressResult:
					addr = msg.Address
				case StreamDDied:
					readOnceMore = true
				default:
					return fmt.Errorf("got unexpected type '%T' instead of %T", content, GetStreamdAddressResult{})
				}
				return nil
			},
		)
		assertNoError(err)

		if readOnceMore {
			continue
		}
		break
	}

	return addr
}
