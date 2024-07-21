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

	setReady(ctx, mainProcess)
	streamdAddr := getStreamDAddress(ctx, mainProcess)

	go func() {
		err := mainProcess.Serve(
			ctx,
			func(ctx context.Context, source mainprocess.ProcessName, content any) error {
				switch content.(type) {
				case StreamDDied:
					logger.Errorf(ctx, "streamd died, killing myself as well (to get reborn)")
					os.Exit(0)
				}
				return nil
			},
		)
		logger.Fatalf(ctx, "communication (with the main process) error: %v", err)
	}()

	logger.Debugf(ctx, "streamd remote address is %s", streamdAddr)
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
	logger.Debugf(ctx, "getStreamDAddress")
	defer logger.Debugf(ctx, "/getStreamDAddress")

	logger.Debugf(ctx, "requesting the streamd address")
	err := mainProcess.SendMessage(ctx, ProcessNameStreamd, GetStreamdAddress{})
	logger.Debugf(ctx, "/requesting the streamd address: %v", err)
	assertNoError(err)

	var addr string
	for {
		readOnceMore := false
		logger.Debugf(ctx, "waiting for the streamd address")
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
		logger.Debugf(ctx, "/waiting for the streamd address: readOnceMore:%v err:%v", readOnceMore, err)
		assertNoError(err)

		if readOnceMore {
			continue
		}
		break
	}

	return addr
}
