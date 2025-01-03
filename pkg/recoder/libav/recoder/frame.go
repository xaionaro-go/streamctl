package recoder

import (
	"time"

	"github.com/asticode/go-astiav"
)

type Frame struct {
	*astiav.Frame
	InputStream        *astiav.Stream
	InputFormatContext *astiav.FormatContext
	DecoderContext     *astiav.CodecContext
	Packet             *astiav.Packet
}

func (f *Frame) MaxPosition() time.Duration {
	return toDuration(f.InputFormatContext.Duration(), 1/float64(astiav.TimeBase))
}

func (f *Frame) Position() time.Duration {
	return toDuration(f.Pts(), f.InputStream.TimeBase().Float64())
}

func (f *Frame) PositionInBytes() int64 {
	return f.Packet.Pos()
}

func (f *Frame) FrameDuration() time.Duration {
	return toDuration(f.Packet.Duration(), f.InputStream.TimeBase().Float64())
}

func toDuration(ts int64, timeBase float64) time.Duration {
	seconds := float64(ts) * float64(timeBase)
	return time.Duration(float64(time.Second) * seconds)
}

type FrameReader interface {
	ReadFrame(frame *Frame) error
}
