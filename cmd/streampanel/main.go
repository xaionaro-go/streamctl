package main

import (
	"context"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/zap"
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/streamctl/pkg/streampanel"
)

func main() {
	loggerLevel := logger.LevelWarning
	pflag.Var(&loggerLevel, "log-level", "Log level")
	configPath := pflag.String("config-path", "~/.streampanel.yaml", "the path to the config file")
	pflag.Parse()

	l := zap.Default().WithLevel(loggerLevel)
	ctx := context.Background()
	ctx = logger.CtxWithLogger(ctx, l)
	logger.Default = func() logger.Logger {
		return l
	}
	defer belt.Flush(ctx)

	l.Fatal(streampanel.New(*configPath).Loop(ctx))
}
