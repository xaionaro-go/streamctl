package streamserver

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/streamforward"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/streamplayers"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types/streamportserver"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

type StreamServer struct {
	*streamplayers.StreamPlayers
	*streamforward.StreamForwards
}

var _ streamforward.StreamServer = (*StreamServer)(nil)

func New() *StreamServer {
	return &StreamServer{}
}

func (srv *StreamServer) WithConfig(
	ctx context.Context,
	callback func(context.Context, *types.Config),
) {
	logger.Tracef(ctx, "WithConfig")
	defer func() { logger.Tracef(ctx, "WithConfig") }()
}

func (srv *StreamServer) WaitPublisherChan(
	ctx context.Context,
	streamID streamtypes.StreamID,
	waitForNext bool,
) (_ <-chan types.Publisher, _err error) {
	logger.Tracef(ctx, "WaitPublisherChan(ctx, '%s', %t)", streamID, waitForNext)
	defer func() { logger.Tracef(ctx, "/WaitPublisherChan(ctx, '%s', %t): %v", streamID, waitForNext, _err) }()
	return nil, fmt.Errorf("not implemented")
}

func (srv *StreamServer) PubsubNames() (_ret types.AppKeys, _err error) {
	ctx := context.TODO()
	logger.Tracef(ctx, "PubsubNames()")
	defer func() { logger.Tracef(ctx, "/PubsubNames(): %v %v", _ret, _err) }()
	return nil, fmt.Errorf("not implemented")
}

func (srv *StreamServer) GetPortServers(ctx context.Context) (_ret []streamportserver.Config, _err error) {
	logger.Tracef(ctx, "GetPortServers()")
	defer func() { logger.Tracef(ctx, "/GetPortServers(): %v %v", _ret, _err) }()
	return nil, fmt.Errorf("not implemented")
}
