package recoder

import (
	"context"
	"sync/atomic"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/streamctl/pkg/recoder"
	"github.com/xaionaro-go/streamctl/pkg/recoder/libav/recoder/types"
)

type EncoderInput struct {
	Input  *Input
	Packet *astiav.Packet
}

type Encoder interface {
	recoder.Encoder

	Encode(
		ctx context.Context,
		input EncoderInput,
	) (*EncoderOutput, error)
}

type EncoderOutput struct {
	*astiav.Packet
	*astiav.Stream
}

func (o *EncoderOutput) UnrefAndFree() {
	o.Packet.Unref()
	o.Packet.Free()
}

type EncoderFramesStatistics = types.EncoderFramesStatistics
type EncoderStatistics = types.EncoderStatistics

type CommonsEncoderFramesStatistics struct {
	Unparsed         atomic.Uint64
	VideoUnprocessed atomic.Uint64
	AudioUnprocessed atomic.Uint64
	VideoProcessed   atomic.Uint64
	AudioProcessed   atomic.Uint64
}

type CommonsEncoderStatistics struct {
	BytesCountRead  atomic.Uint64
	BytesCountWrote atomic.Uint64
	FramesRead      CommonsEncoderFramesStatistics
	FramesWrote     CommonsEncoderFramesStatistics
}

func (stats *CommonsEncoderStatistics) Convert() EncoderStatistics {
	return EncoderStatistics{
		BytesCountRead:  stats.BytesCountRead.Load(),
		BytesCountWrote: stats.BytesCountWrote.Load(),
		FramesRead: EncoderFramesStatistics{
			Unparsed:         stats.FramesRead.Unparsed.Load(),
			VideoUnprocessed: stats.FramesRead.VideoUnprocessed.Load(),
			AudioUnprocessed: stats.FramesRead.AudioUnprocessed.Load(),
			VideoProcessed:   stats.FramesRead.VideoProcessed.Load(),
			AudioProcessed:   stats.FramesRead.AudioProcessed.Load(),
		},
		FramesWrote: EncoderFramesStatistics{
			Unparsed:         stats.FramesWrote.Unparsed.Load(),
			VideoUnprocessed: stats.FramesWrote.VideoUnprocessed.Load(),
			AudioUnprocessed: stats.FramesWrote.AudioUnprocessed.Load(),
			VideoProcessed:   stats.FramesWrote.VideoProcessed.Load(),
			AudioProcessed:   stats.FramesWrote.AudioProcessed.Load(),
		},
	}
}

type CommonsEncoder struct {
	CommonsEncoderStatistics
}

func (e *CommonsEncoder) GetStats() *EncoderStatistics {
	return ptr(e.CommonsEncoderStatistics.Convert())
}
