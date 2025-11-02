package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/davecgh/go-spew/spew"
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

func must[T any](v T, err error) T {
	assertNoError(err)
	return v
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

	h := must(kick.NewChatHandlerOBSOLETE(ctx, channelSlug))
	msgCh := must(h.GetMessagesChan(ctx))

	fmt.Println("started")
	for ev := range msgCh {
		spew.Dump(ev)
	}
}
