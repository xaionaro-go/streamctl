package streamserver

import (
	"github.com/xaionaro-go/streamctl/pkg/streamserver/implementations/yutopp-go-rtmp/streamserver"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
)

type StreamServer interface {
	types.StreamServer[ActiveStreamForwarding]
}
type PlatformsController = types.PlatformsController
type BrowserOpener = types.BrowserOpener

func New(
	cfg *types.Config,
	platformsController types.PlatformsController,
	browserOpener BrowserOpener,
) StreamServer {
	return streamserver.New(cfg, platformsController, browserOpener)
}
