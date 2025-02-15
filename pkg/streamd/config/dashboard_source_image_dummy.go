package config

import (
	"context"
	"image"
	"time"
)

type DashboardSourceImageDummy struct{}

func (*DashboardSourceImageDummy) GetImage(
	ctx context.Context,
	el DashboardElementConfig,
	_ ImageDataProvider,
) (image.Image, time.Time, error) {
	img := image.NewRGBA(image.Rectangle{
		Min: image.Point{
			X: 0,
			Y: 0,
		},
		Max: image.Point{
			X: 0,
			Y: 0,
		},
	})
	return img, time.Time{}, nil
}

func (*DashboardSourceImageDummy) SourceType() DashboardSourceImageType {
	return DashboardSourceImageTypeDummy
}
