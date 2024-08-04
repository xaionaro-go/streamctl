package main

import (
	"context"
	"fmt"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/mainprocess"
)

func forkUI(preCtx context.Context, mainProcessAddr, password string) {
	procName := ProcessNameUI

	mainProcess, err := mainprocess.NewClient(
		procName,
		mainProcessAddr,
		password,
	)
	if err != nil {
		panic(err)
	}
	flags := getFlags(preCtx, mainProcess)
	ctx := getContext(flags)
	ctx = belt.WithField(ctx, "process", procName)
	logger.Debugf(ctx, "flags == %#+v", flags)
	ctx, cancelFunc := initRuntime(ctx, flags, procName)
	defer cancelFunc()
	childProcessSignalHandler(ctx, cancelFunc)

	streamdAddr := getStreamDAddress(ctx, mainProcess)

	logger.Debugf(ctx, "streamd remote address is %s", streamdAddr)
	flags.RemoteAddr = streamdAddr

	ctx = belt.WithField(ctx, "streamd_addr", flags.RemoteAddr)
	l := logger.FromCtx(ctx)
	logger.Default = func() logger.Logger {
		return l
	}

	logger.Infof(ctx, "running the UI")
	runPanel(ctx, cancelFunc, flags, mainProcess)
	err = mainProcess.SendMessage(ctx, ProcessNameMain, MessageQuit{})
	if err != nil {
		logger.Error(ctx, "unable to send the Quit message to the main process: %w", err)
	}
	<-ctx.Done() // wait for get killed by the main process (should happen in matter of milliseconds)
	time.Sleep(5 * time.Second)
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
