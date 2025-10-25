package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	xlogrus "github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
)

func assertNoError(err error) {
	if err == nil {
		return
	}
	log.Panic(err)
}

func main() {
	logLevel := logger.LevelInfo
	pflag.Var(&logLevel, "log-level", "")
	pflag.Parse()

	if pflag.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "expected 1 argument\n")
		os.Exit(1)
	}

	channelSlug := pflag.Arg(0)

	ctx := logger.CtxWithLogger(context.Background(), xlogrus.Default().WithLevel(logLevel))
	logger.Default = func() logger.Logger {
		return logger.FromCtx(ctx)
	}
	defer belt.Flush(ctx)

	h, err := kick.NewChatHandlerOBSOLETE(ctx, channelSlug)
	assertNoError(err)

	msgCh, err := h.GetMessagesChan(ctx)
	assertNoError(err)

	fmt.Println("started")
	for ev := range msgCh {
		fmt.Printf("%#+v\n", ev)
	}
}
