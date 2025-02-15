package config

import (
	"context"
	"image"
	"time"

	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

type GetImageBytesFromOBSer interface {
	GetImageBytesFromOBS(
		ctx context.Context,
		obsServer obs_grpc.OBSServer,
		el DashboardElementConfig,
	) ([]byte, string, time.Time, error)
}

type GetImageFromOBSer interface {
	GetImageFromOBS(
		ctx context.Context,
		obsServer obs_grpc.OBSServer,
		el DashboardElementConfig,
		obsState *streamtypes.OBSState,
	) (image.Image, time.Time, error)
}
