package streamserver

import (
	"github.com/xaionaro-go/streamctl/pkg/streamserver/implementations/yutopp-go-rtmp/streamserver"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
)

type StreamServer = streamserver.StreamServer
type PlatformsController = streamserver.PlatformsController
type BrowserOpener = streamserver.BrowserOpener

func New(
	cfg *types.Config,
	platformsController streamserver.PlatformsController,
	browserOpener BrowserOpener,
) *StreamServer {
	return streamserver.New(cfg, platformsController, browserOpener)
}
