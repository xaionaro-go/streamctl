package config

import (
	"context"
	"image"
	"time"

	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

type DashboardSourceImageDummy struct{}

func (*DashboardSourceImageDummy) GetImage(
	ctx context.Context,
	obsServer obs_grpc.OBSServer,
	el DashboardElementConfig,
	obsState *streamtypes.OBSState,
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
