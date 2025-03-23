package screenshot

import (
	"fmt"
	"image"

	"github.com/kbinani/screenshot"
)

func Screenshot(cfg Config) (*image.RGBA, error) {
	rgbaFull, err := screenshot.Capture(
		cfg.Bounds.Min.X,
		cfg.Bounds.Min.Y,
		cfg.Bounds.Dx(),
		cfg.Bounds.Dy(),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to screenshot bounds %v: %w", cfg.Bounds, err)
	}

	return rgbaFull, nil
}
