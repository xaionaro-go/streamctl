package streamd

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
	"github.com/xaionaro-go/recoder"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

type imageDataProvider struct {
	OBSServer obs_grpc.OBSServer
	OBSState  *OBSState
}

var _ config.ImageDataProvider = (*imageDataProvider)(nil)

func newImageDataProvider(
	obsServer obs_grpc.OBSServer,
	obsState *OBSState,
) *imageDataProvider {
	return &imageDataProvider{
		OBSServer: obsServer,
		OBSState:  obsState,
	}
}

func (img *imageDataProvider) GetOBSServer(
	ctx context.Context,
) (obs_grpc.OBSServer, error) {
	return img.OBSServer, nil
}

func (img *imageDataProvider) GetOBSState(
	ctx context.Context,
) (*streamtypes.OBSState, error) {
	return img.OBSState, nil
}

func (img *imageDataProvider) GetCurrentStreamFrame(
	ctx context.Context,
	streamID streamtypes.StreamID,
) ([]byte, recoder.VideoCodec, error) {
	return nil, 0, fmt.Errorf("not implemented")
}
