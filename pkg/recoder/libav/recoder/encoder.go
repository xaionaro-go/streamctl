package recoder

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/streamctl/pkg/recoder"
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
	refCounter sync.WaitGroup

	OverrideUnrefAndFreeFunc func()
}

func (o *EncoderOutput) UnrefAndFree() {
	if o.OverrideUnrefAndFreeFunc != nil {
		o.OverrideUnrefAndFreeFunc()
		return
	}

	o.Packet.Unref()
	o.Packet.Free()
}

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

type CommonsEncoder struct {
	CommonsEncoderStatistics
}

func (e *CommonsEncoder) GetStats() *CommonsEncoderStatistics {
	return &e.CommonsEncoderStatistics
}
