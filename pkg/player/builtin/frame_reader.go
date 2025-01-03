package builtin

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/recoder/libav/recoder"
)

type frameReader struct {
	Context context.Context
	Player  *Player
}

func (p *Player) newFrameReader(ctx context.Context) *frameReader {
	return &frameReader{
		Context: ctx,
		Player:  p,
	}
}

func (fr *frameReader) ReadFrame(frame *recoder.Frame) error {
	return fr.Player.processFrame(fr.Context, frame)
}
