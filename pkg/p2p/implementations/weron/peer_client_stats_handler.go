package weron

import (
	"context"

	"github.com/facebookincubator/go-belt/tool/logger"
	"google.golang.org/grpc/stats"
)

type peerClientStatsHandler struct {
	*peerClient
}

func (p *peerClientStatsHandler) TagRPC(ctx context.Context, info *stats.RPCTagInfo) context.Context {
	logger.Tracef(ctx, "TagRPC(ctx, %#+v)", info)
	return ctx
}
func (p *peerClientStatsHandler) HandleRPC(ctx context.Context, stats stats.RPCStats) {
	logger.Tracef(ctx, "HandleRPC(ctx, %#+v)", stats)
}
func (p *peerClientStatsHandler) TagConn(ctx context.Context, info *stats.ConnTagInfo) context.Context {
	logger.Debugf(ctx, "TagConn(ctx, %#+v)", info)
	return ctx
}
func (p *peerClientStatsHandler) HandleConn(ctx context.Context, stats stats.ConnStats) {
	logger.Debugf(ctx, "HandleConn(ctx, %#+v)", stats)
}
