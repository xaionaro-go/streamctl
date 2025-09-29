//go:build !linux
// +build !linux

package screenshoter

import (
	"context"
	"fmt"
	"image"
	"time"

	"github.com/xaionaro-go/streamctl/pkg/screenshot"
)

type ScreenshoterWayland struct {
	Display string
}

func (s *ScreenshoterWayland) Loop(
	ctx context.Context,
	interval time.Duration,
	config screenshot.Config,
	callback func(context.Context, image.Image),
) error {
	return fmt.Errorf("Wayland is not supported on non-Unix-like systems")
}
