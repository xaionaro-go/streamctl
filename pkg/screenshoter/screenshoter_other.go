package screenshoter

import (
	"context"
	"image"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/screenshot"
)

type ScreenshoterOther struct {
	ScreenshotEngine ScreenshotEngine
}

func (s *ScreenshoterOther) Loop(
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
