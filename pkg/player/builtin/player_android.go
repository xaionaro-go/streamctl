//go:build android
// +build android

package builtin

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/recoder/libav/recoder"
)

func (p *Player) processFrame(
	ctx context.Context,
	frame *recoder.Frame,
) error {
	panic("not implemented")
}
