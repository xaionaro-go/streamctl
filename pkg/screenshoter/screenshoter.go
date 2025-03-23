package screenshoter

import (
	"context"
	"image"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/screenshot"
)

type ScreenshotEngine interface {
	Screenshot(cfg screenshot.Config) (*image.RGBA, error)
}

type Screenshoter struct {
	ScreenshotEngine ScreenshotEngine
}

func New(
	engine ScreenshotEngine,
) *Screenshoter {
	return &Screenshoter{
		ScreenshotEngine: engine,
	}
}

func (s *Screenshoter) Engine() ScreenshotEngine {
	return s.ScreenshotEngine
}

func (s *Screenshoter) Loop(
	ctx context.Context,
	interval time.Duration,
	config screenshot.Config,
	callback func(context.Context, *image.RGBA),
) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		img, err := s.ScreenshotEngine.Screenshot(config)
		if err != nil {
			logger.Errorf(ctx, "unable to take a screenshot: %v", err)
			continue
		}
		callback(ctx, img)
	}
}
