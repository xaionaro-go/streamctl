package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/facebookincubator/go-belt/tool/logger"
)

func signalHandler(
	ctx context.Context,
) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for range c {
			forkLocker.Lock()
			for name, f := range forkMap {
				logger.Debugf(ctx, "killing '%s'", name)
				err := f.Process.Kill()
				if err != nil {
					logger.Error(ctx, "unable to kill '%s': %w", name, err)
				}
			}
			forkLocker.Unlock()
			os.Exit(1)
		}
	}()
}
