package screenshoter

import (
	"context"
	"image"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/screenshot"
)

type ScreenshotEngine interface {
	Screenshot(cfg screenshot.Config) (image.Image, error)
}

type Screenshoter struct {
	ScreenshotEngine ScreenshotEngine
}

type ScreenshotImplementation struct{}

func (ScreenshotImplementation) Screenshot(cfg screenshot.Config) (image.Image, error) {
	return screenshot.Implementation{}.Screenshot(cfg)
}

func New() *Screenshoter {
	return &Screenshoter{
		ScreenshotEngine: ScreenshotImplementation{},
	}
}

// TODO: add the support of Wayland
func (s *Screenshoter) Loop(
	ctx context.Context,
	interval time.Duration,
	config screenshot.Config,
	callback func(context.Context, image.Image),
) error {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
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
