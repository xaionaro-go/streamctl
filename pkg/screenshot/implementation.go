package screenshot

import (
	"image"
)

type Implementation struct{}

func (Implementation) NumActiveDisplays() uint {
	return NumActiveDisplays()
}

func (Implementation) Screenshot(cfg Config) (*image.RGBA, error) {
	return Screenshot(cfg)
}
