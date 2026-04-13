package main

import (
	"context"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/zap"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/cmd/streamcli/commands"
)

func main() {
	l := zap.Default()
	// Attach the global LogLevelFilter as a pre-hook so that logger.Default()
	// respects the log level set in PersistentPreRun. Without this, code using
	// logger.Default() (e.g. config parsing) bypasses the context-based level.
	observability.LogLevelFilter.SetLevel(commands.LoggerLevel)
	l = l.WithPreHooks(&observability.LogLevelFilter)
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
