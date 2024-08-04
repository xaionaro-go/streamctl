package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/observability"
)

func signalHandler(
	ctx context.Context,
) chan<- os.Signal {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	observability.Go(ctx, func() {
		for range c {
			forkLocker.Lock()
			for name, f := range forkMap {
				logger.Debugf(ctx, "killing '%s'", name)
				err := f.Process.Kill()
				if err != nil {
					logger.Errorf(ctx, "unable to kill '%s': %v", name, err)
				}
			}
			forkLocker.Unlock()
			os.Exit(1)
		}
	})
	return c
}
