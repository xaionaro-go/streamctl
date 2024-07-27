package main

import (
	"context"
	"fmt"

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

	logger.Debugf(ctx, "streamd remote address is %s", streamdAddr)
	flags.RemoteAddr = streamdAddr
	runPanel(ctx, flags, mainProcess)
	err = mainProcess.SendMessage(ctx, ProcessNameMain, MessageQuit{})
	if err != nil {
		logger.Error(ctx, "unable to send the Quit message to the main process: %w", err)
	}

	logger.Infof(ctx, "UI is ready")
	<-ctx.Done()
}

type MessageQuit struct{}
type StreamDDied struct{}
type GetStreamdAddress struct{}
type GetStreamdAddressResult struct {
	Address string
}

func getStreamDAddress(
	ctx context.Context,
	mainProcess *mainprocess.Client,
) string {
	logger.Debugf(ctx, "getStreamDAddress")
	defer logger.Debugf(ctx, "/getStreamDAddress")

	setReadyFor(ctx, mainProcess, GetStreamdAddressResult{})

	logger.Debugf(ctx, "requesting the streamd address")
	err := mainProcess.SendMessage(ctx, ProcessNameStreamd, GetStreamdAddress{})
	logger.Debugf(ctx, "/requesting the streamd address: %v", err)
	assertNoError(err)

	var addr string
	logger.Debugf(ctx, "waiting for the streamd address")
	err = mainProcess.ReadOne(
		ctx,
		func(ctx context.Context, source mainprocess.ProcessName, content any) error {
			switch msg := content.(type) {
			case GetStreamdAddressResult:
				addr = msg.Address
			default:
				return fmt.Errorf("got unexpected type '%T' instead of %T", content, GetStreamdAddressResult{})
			}
			return nil
		},
	)
	logger.Debugf(ctx, "/waiting for the streamd address: err:%v", err)
	assertNoError(err)

	return addr
}
