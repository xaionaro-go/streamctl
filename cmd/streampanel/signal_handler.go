package main

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/observability"
)

func mainProcessSignalHandler(
	ctx context.Context,
	cancelFn context.CancelFunc,
) chan<- os.Signal {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	observability.Go(ctx, func() {
		for range c {
			cancelFn()
			forkLocker.Lock()
			var wg sync.WaitGroup
			for name, f := range forkMap {
				wg.Add(1)
				{
					name, f := name, f
					observability.Go(ctx, func() {
						defer wg.Done()
						logger.Debugf(ctx, "interrupting '%s'", name)
						err := f.Process.Signal(os.Interrupt)
						if err != nil {
							logger.Errorf(ctx, "unable to send Interrupt to '%s': %v", name, err)
							logger.Debugf(ctx, "killing '%s'", name)
							f.Process.Kill()
							return
						}

						observability.Go(ctx, func() {
							time.Sleep(5 * time.Second)
							logger.Debugf(ctx, "killing '%s'", name)
							err := f.Process.Kill()
							if err != nil {
								logger.Errorf(ctx, "unable to kill '%s': %v", name, err)
							}
						})
						err = f.Wait()
						if err != nil {
							logger.Errorf(ctx, "unable to wait for '%s': %v", name, err)
						}
					})
				}
			}
			wg.Wait()
			forkLocker.Unlock()
			cancelFn()
			os.Exit(0)
		}
	})
	return c
}

func childProcessSignalHandler(
	ctx context.Context,
	cancelFunc context.CancelFunc,
) chan<- os.Signal {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	observability.Go(ctx, func() {
		for range c {
			logger.Infof(ctx, "received an interruption signal")
			cancelFunc()
			time.Sleep(100 * time.Millisecond) // TODO: delete this hack
			os.Exit(0)
		}
	})
	return c
}
