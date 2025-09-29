package screenshoter

import (
	"context"
	"image"
	"os"
	"time"

	"github.com/xaionaro-go/streamctl/pkg/screenshot"
)

type Screenshoter interface {
	Loop(
		ctx context.Context,
		interval time.Duration,
		config screenshot.Config,
		callback func(context.Context, image.Image),
	) error
}

type ScreenshotEngine interface {
	Screenshot(cfg screenshot.Config) (image.Image, error)
}

type ScreenshotImplementation struct{}

func (ScreenshotImplementation) Screenshot(cfg screenshot.Config) (image.Image, error) {
	return screenshot.Implementation{}.Screenshot(cfg)
}

func New() Screenshoter {
	switch {
	case os.Getenv("WAYLAND_DISPLAY") != "":
		return &ScreenshoterWayland{
			Display: os.Getenv("WAYLAND_DISPLAY"),
		}
	default:
		return &ScreenshoterOther{
			ScreenshotEngine: ScreenshotImplementation{},
		}
	}
}
