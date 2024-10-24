package audio

import (
	"fmt"
	"io"
	"time"
)

type PlayerPCM interface {
	Ping() error
	PlayPCM(
		sampleRate uint32,
		channels uint16,
		format PCMFormat,
		bufferSize time.Duration,
		reader io.Reader,
	) error
}

type PCMFormat uint

const (
	PCMFormatUndefined = PCMFormat(iota)
	PCMFormatFloat32LE
)

func (f *PCMFormat) String() string {
	if f == nil {
		return "null"
	}

	switch *f {
	case PCMFormatUndefined:
		return "<undefined>"
	case PCMFormatFloat32LE:
		return "f32le"
	default:
		return fmt.Sprintf("<unexpected_value_%d>", *f)
	}
}
