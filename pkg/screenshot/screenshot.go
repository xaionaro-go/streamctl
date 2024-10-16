package screenshot

import (
	"fmt"
	"image"

	"github.com/kbinani/screenshot"
)

func Screenshot(cfg Config) (*image.RGBA, error) {
	activeDisplays := screenshot.NumActiveDisplays()
	if cfg.DisplayID >= uint(activeDisplays) {
		return nil, fmt.Errorf(
			"display ID %d is out of range (max: %d)",
			cfg.DisplayID,
			activeDisplays-1,
		)
	}

	rgbaFull, err := screenshot.CaptureDisplay(int(cfg.DisplayID))
	if err != nil {
		return nil, fmt.Errorf("unable to screenshot screen %d: %w", cfg.DisplayID, err)
	}

	var rgbaCropped *image.RGBA
	resizeTo := cfg.Bounds
	if resizeTo.Max.X > 0 && resizeTo.Max.Y > 0 {
		var ok bool
		rgbaCropped, ok = rgbaFull.SubImage(resizeTo).(*image.RGBA)
		if !ok {
			return nil, fmt.Errorf("got type %T, but expected %T", rgbaCropped, (*image.RGBA)(nil))
		}
	} else {
		rgbaCropped = rgbaFull
	}

	return rgbaCropped, nil
}
