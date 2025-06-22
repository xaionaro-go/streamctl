package streampanel

import (
	"context"
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
)

func (p *Panel) initFyneHacks(ctx context.Context) {
	p.initWindowsHealthChecker(ctx)
}

func (p *Panel) initWindowsHealthChecker(ctx context.Context) {
	observability.Go(ctx, func(ctx context.Context) {
		logger.Debugf(ctx, "initWindowsHealthChecker")
		defer logger.Debugf(ctx, "/initWindowsHealthChecker")

		t := time.NewTicker(7 * time.Second / 3)
		defer t.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				p.checkAndFixWindowsHealth(ctx)
			}
		}
	})
}

func (p *Panel) newPermanentWindow(
	ctx context.Context,
	title string,
) fyne.Window {
	w := p.app.NewWindow(title)
	if w == nil {
		return nil
	}

	p.addPermanentWindow(ctx, w)
	return w
}

func (p *Panel) addPermanentWindow(
	ctx context.Context,
	window fyne.Window,
) {
	drv, ok := window.(windowDriver)
	if !ok {
		logger.Warnf(ctx, "window does not implement the expected interface `windowDriver`: %T", window)
		return
	}

	p.windowsLocker.Do(ctx, func() {
		p.permanentWindows[p.windowsCounter.Add(1)] = drv
	})
}

func (p *Panel) checkAndFixWindowsHealth(ctx context.Context) {
	p.windowsLocker.Do(ctx, func() {
		for _, window := range p.permanentWindows {
			checkAndFixPermanentWindowHealth(ctx, window)
		}
	})
}

func checkAndFixPermanentWindowHealth(
	ctx context.Context,
	window windowDriver,
) {
	err := window.QueueEvent(func() {})
	if err == nil {
		return
	}

	logger.Warnf(ctx, "window %v has a broken event queue, fixing")
	window.InitEventQueue()
	observability.Go(ctx, func(ctx context.Context) {
		window.RunEventQueue()
	})
}

type windowDriver interface {
	InitEventQueue()
	QueueEvent(fn func()) error
	RunEventQueue()
	//DestroyEventQueue()
	//WaitForEvents()
}

func fyneTryLoop(ctx context.Context, fn func()) {
	var r any
	for i := 0; i < 10; i++ {
		err := func() (_err error) {
			defer func() {
				r = recover()
				if r == nil {
					return
				}
				_err = fmt.Errorf("got panic: %v", r)
			}()
			fn()
			return nil
		}()
		if err == nil {
			return
		}
		logger.Debugf(ctx, "got error: %v", err)
		time.Sleep(time.Millisecond * 100)
	}
	panic(r)
}
