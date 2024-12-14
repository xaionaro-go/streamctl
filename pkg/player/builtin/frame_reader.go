package builtin

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/encoder/libav/encoder"
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

func (fr *frameReader) ReadFrame(frame *encoder.Frame) error {
	return fr.Player.processFrame(fr.Context, frame)
}
