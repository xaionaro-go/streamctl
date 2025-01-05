//go:build !windows
// +build !windows

package recoder

import (
	"context"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/libsrt/extras/xastiav"
	"github.com/xaionaro-go/libsrt/threadsafe"
)

func formatContextToSRTSocket(
	ctx context.Context,
	fmtCtx *astiav.FormatContext,
) (_ret *threadsafe.Socket, _err error) {
	logger.Tracef(ctx, "formatContextToSRTSocket")
	defer func() { logger.Tracef(ctx, "/formatContextToSRTSocket: %v %v", _ret, _err) }()

	sockC := xastiav.GetFDFromFormatContext(fmtCtx)
	logger.Debugf(ctx, "SRT file descriptor: %d", sockC)
	sock := threadsafe.SocketFromC(int32(sockC))
	return sock, nil
}
