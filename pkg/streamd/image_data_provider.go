package streamd

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
	"github.com/xaionaro-go/recoder"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
	"github.com/xaionaro-go/xsync"
)

type imageDataProvider struct {
	StreamD                *StreamD
	OBSServer              obs_grpc.OBSServer
	StreamImageTakerLocker xsync.Mutex
	StreamImageTakes       map[streamtypes.StreamSourceID]*streamImageTaker
}

var _ config.ImageDataProvider = (*imageDataProvider)(nil)

func newImageDataProvider(
	streamD *StreamD,
	obsServer obs_grpc.OBSServer,
) *imageDataProvider {
	return &imageDataProvider{
		StreamD:   streamD,
		OBSServer: obsServer,
	}
}

func (p *imageDataProvider) GetOBSServer(
	ctx context.Context,
) (obs_grpc.OBSServer, error) {
	return p.OBSServer, nil
}

func (p *imageDataProvider) GetOBSState(
	ctx context.Context,
) (*streamtypes.OBSState, error) {
	return &p.StreamD.OBSState, nil
}

func (p *imageDataProvider) GetCurrentStreamFrame(
	ctx context.Context,
	streamSourceID streamtypes.StreamSourceID,
) ([]byte, recoder.VideoCodec, error) {
	return xsync.DoR3(ctx, &p.StreamImageTakerLocker, func() ([]byte, recoder.VideoCodec, error) {
		streamImageTaker := p.StreamImageTakes[streamSourceID]
		if streamImageTaker == nil || !streamImageTaker.Keepalive() {
			var err error
			streamImageTaker, err = p.newStreamImageTaker(ctx, streamSourceID)
			if err != nil {
				return nil, 0, fmt.Errorf("unable to initialize a stream image taker: %w", err)
			}
		}
		return streamImageTaker.GetLastFrame(ctx)
	})
}
