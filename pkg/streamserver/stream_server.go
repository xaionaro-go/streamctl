package streamserver

import (
	"github.com/xaionaro-go/streamctl/pkg/streamserver/implementations/yutopp-go-rtmp/streamserver"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
)

type StreamServer = streamserver.StreamServer

func New(
	cfg *types.Config,
	platformsController streamserver.PlatformsController,
) *StreamServer {
	return streamserver.New(cfg, platformsController)
}
