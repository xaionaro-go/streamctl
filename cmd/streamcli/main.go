package main

import (
	"context"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/zap"
	"github.com/xaionaro-go/streamctl/cmd/streamcli/commands"
)

func main() {
	l := zap.Default()
	ctx := context.Background()
	ctx = logger.CtxWithLogger(ctx, l)
	logger.Default = func() logger.Logger {
		return l
	}
	defer belt.Flush(ctx)

	err := commands.Root.ExecuteContext(ctx)
	if err != nil {
		logger.Panic(ctx, err)
	}
}
