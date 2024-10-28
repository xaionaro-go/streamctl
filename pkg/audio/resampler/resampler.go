package resampler

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"sync"

	"github.com/xaionaro-go/streamctl/pkg/audio/types"
)

type Format struct {
	Channels   uint16
	SampleRate uint32
	PCMFormat  types.PCMFormat
}

type precalculated struct {
	inSampleSize  uint
	outSampleSize uint
	inNumAvg      uint
	outNumRepeat  uint
	convert       func(dst, src []byte)
}

type Resampler struct {
	inReader  io.Reader
	inFormat  Format
	outFormat Format
	locker    sync.Mutex
	buffer    []byte
	precalculated
}

var pcmSampleConvertMap = [types.EndOfPCMFormat + 1] /*from*/ [types.EndOfPCMFormat + 1] /*to*/ func(dst, src []byte){
	types.PCMFormatU8: [types.EndOfPCMFormat + 1]func(dst, src []byte){
		types.PCMFormatU8: func(dst, src []byte) {
			copy(dst, src)
		},
		types.PCMFormatFloat32LE: func(dst, src []byte) {
			v := float32(src[0])
			binary.LittleEndian.PutUint32(dst, math.Float32bits(v))
		},
	},
	types.PCMFormatFloat32LE: [types.EndOfPCMFormat + 1]func(dst, src []byte){
		types.PCMFormatFloat32LE: func(dst, src []byte) {
			copy(dst, src)
		},
	},
}

var _ io.Reader = (*Resampler)(nil)

func NewResampler(
	inFormat Format,
	inReader io.Reader,
	outFormat Format,
) (*Resampler, error) {
	r := &Resampler{
		inReader:  inReader,
		inFormat:  inFormat,
		outFormat: outFormat,
	}
	err := r.init()
	if err != nil {
		return nil, fmt.Errorf("unable to initialize a resampler from %#+v to %#+v: %w", inFormat, outFormat, err)
	}
	return r, nil
}

func (r *Resampler) init() error {
	r.inSampleSize = uint(r.inFormat.PCMFormat.Size())
	r.outSampleSize = uint(r.outFormat.PCMFormat.Size())

	r.inNumAvg = 1
	r.outNumRepeat = 1
	if r.inFormat.Channels != r.outFormat.Channels {
		switch {
		case r.inFormat.Channels == 1:
			r.outNumRepeat = uint(r.outFormat.Channels)
		case r.outFormat.Channels == 1:
			r.inNumAvg = uint(r.outFormat.Channels)
		default:
			return fmt.Errorf("do not know how to convert %d channels to %d", r.inFormat.Channels, r.outFormat.Channels)
		}
	}

	if r.inNumAvg != 1 {
		return fmt.Errorf("the case with reducing the amount of channels is not implemented, yet")
	}

	r.convert = pcmSampleConvertMap[r.inFormat.PCMFormat][r.outFormat.PCMFormat]
	return nil
}

func (r *Resampler) Read(p []byte) (int, error) {
	r.locker.Lock()
	defer r.locker.Unlock()
	chunksToRead := len(p) / int(r.outSampleSize) / int(r.outNumRepeat)
	bytesToRead := chunksToRead * int(r.inSampleSize)
	if cap(r.buffer) < bytesToRead {
		r.buffer = make([]byte, bytesToRead)
	} else {
		r.buffer = r.buffer[:bytesToRead]
	}
	n, err := r.inReader.Read(r.buffer)
	r.buffer = r.buffer[:n]

	if n%int(r.inSampleSize) != 0 {
		return 0, fmt.Errorf("read a number of bytes that is not a multiple of %d: %w", r.inSampleSize, err)
	}
	chunksToWrite := n / int(r.inSampleSize) / int(r.inNumAvg)

	for chunkIdx := 0; chunkIdx < chunksToWrite; chunkIdx++ {
		// TODO: It is assumed that r.inNumAvg == 1 (see `init()`), so we omit
		//       averaging the value, yet. Fix this.
		idxSrc := chunkIdx * int(r.inSampleSize)
		for repeatIdx := 0; repeatIdx < int(r.outNumRepeat); repeatIdx++ {
			idxDst := (chunkIdx*int(r.outNumRepeat) + repeatIdx) * int(r.outSampleSize)
			r.convert(p[idxDst:], r.buffer[idxSrc:])
		}
	}
	return n * int(r.outNumRepeat), err
}
