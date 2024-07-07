package screenshot

import (
	"fmt"
	"image"

	"github.com/kbinani/screenshot"
)

func Screenshot(cfg Config) (*image.RGBA, error) {
	activeDisplays := screenshot.NumActiveDisplays()
	if cfg.DisplayID >= uint(activeDisplays) {
		return nil, fmt.Errorf("display ID %d is out of range (max: %d)", cfg.DisplayID, activeDisplays-1)
	}

	rgbaFull, err := screenshot.CaptureDisplay(int(cfg.DisplayID))
	if err != nil {
		return nil, fmt.Errorf("unable to screenshot screen %d: %w", cfg.DisplayID, err)
	}

	rgbaCropped, ok := rgbaFull.SubImage(cfg.Bounds).(*image.RGBA)
	if !ok {
		return nil, fmt.Errorf("got type %T, but expected %T", rgbaCropped, (*image.RGBA)(nil))
	}

	return rgbaCropped, nil
}
