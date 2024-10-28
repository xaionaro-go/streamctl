package types

import (
	"fmt"
	"io"
	"math"
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

func (f PCMFormat) Size() uint32 {
	switch f {
	case PCMFormatUndefined:
		return math.MaxUint32
	case PCMFormatFloat32LE:
		return 4
	default:
		return math.MaxUint32
	}
}

func (f PCMFormat) String() string {
	switch f {
	case PCMFormatUndefined:
		return "<undefined>"
	case PCMFormatFloat32LE:
		return "f32le"
	default:
		return fmt.Sprintf("<unexpected_value_%d>", f)
	}
}
