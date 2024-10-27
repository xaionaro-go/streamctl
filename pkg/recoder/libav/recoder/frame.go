package recoder

import (
	"time"

	"github.com/asticode/go-astiav"
)

type Frame struct {
	*astiav.Frame
	DecoderContext *astiav.CodecContext
	Packet         *astiav.Packet
}

func (f *Frame) Position() time.Duration {
	return f.toDuration(f.Pts())
}

func (f *Frame) Duration() time.Duration {
	return f.toDuration(f.Packet.Duration())
}

func (f *Frame) toDuration(ts int64) time.Duration {
	seconds := float64(ts) * f.DecoderContext.TimeBase().Float64()
	return time.Duration(float64(time.Second) * seconds)
}

type FrameReader interface {
	ReadFrame(frame *Frame) error
}
