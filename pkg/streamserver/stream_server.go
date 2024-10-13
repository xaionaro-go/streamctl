package streamserver

import (
	"github.com/xaionaro-go/streamctl/pkg/streamserver/iface"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/implementations/libav/streamforward"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/implementations/mediamtx/streamserver"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
)

type StreamServer = iface.StreamServer
type ActiveStreamForwarding = *streamforward.ActiveStreamForwarding
type PlatformsController = types.PlatformsController
type BrowserOpener = types.BrowserOpener

func New(
	cfg *types.Config,
	platformsController types.PlatformsController,
) StreamServer {
	return streamserver.New(cfg, platformsController)
}
