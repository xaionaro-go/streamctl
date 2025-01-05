//go:build !windows
// +build !windows

package recoder

import (
	"context"

	"github.com/xaionaro-go/libsrt/threadsafe"
)

func (output *Output) SRT(ctx context.Context) (*threadsafe.Socket, error) {
	return formatContextToSRTSocket(ctx, output.FormatContext)
}
