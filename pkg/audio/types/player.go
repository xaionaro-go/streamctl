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
	) (Stream, error)
}

type PCMFormat uint

const (
	PCMFormatUndefined = PCMFormat(iota)
	PCMFormatU8
	PCMFormatS16LE
	PCMFormatS16BE
	PCMFormatFloat32LE
	PCMFormatFloat32BE
	PCMFormatS24LE
	PCMFormatS24BE
	PCMFormatS32LE
	PCMFormatS32BE
	PCMFormatFloat64LE
	PCMFormatFloat64BE
	PCMFormatS64LE
	PCMFormatS64BE
	EndOfPCMFormat
)

func (f PCMFormat) Size() uint32 {
	switch f {
	case PCMFormatUndefined:
		return math.MaxUint32
	case PCMFormatU8:
		return 1
	case PCMFormatS16LE, PCMFormatS16BE:
		return 2
	case PCMFormatS24LE, PCMFormatS24BE:
		return 3
	case PCMFormatFloat32LE, PCMFormatFloat32BE, PCMFormatS32LE, PCMFormatS32BE:
		return 4
	case PCMFormatFloat64LE, PCMFormatFloat64BE, PCMFormatS64LE, PCMFormatS64BE:
		return 8
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
