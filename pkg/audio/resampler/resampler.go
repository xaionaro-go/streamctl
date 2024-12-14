package resampler

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"sync"

	"github.com/xaionaro-go/streamctl/pkg/audio"
	"github.com/xaionaro-go/streamctl/pkg/audio/types"
)

const (
	distanceStep = 10000
)

type Format struct {
	Channels   audio.Channel
	SampleRate audio.SampleRate
	PCMFormat  types.PCMFormat
}

type precalculated struct {
	inSampleSize    uint
	outSampleSize   uint
	inNumAvg        uint
	outNumRepeat    uint
	outDistanceStep uint64
	convert         func(dst, src []byte)
}

type Resampler struct {
	inReader    io.Reader
	inFormat    Format
	outFormat   Format
	inDistance  uint64
	outDistance uint64
	locker      sync.Mutex
	buffer      []byte
	precalculated
}

var pcmSampleConvertMap = [types.EndOfPCMFormat + 1] /*from*/ [types.EndOfPCMFormat + 1] /*to*/ func(dst, src []byte){
	types.PCMFormatU8: {
		types.PCMFormatU8: func(dst, src []byte) {
			copy(dst, src)
		},
		types.PCMFormatFloat32LE: func(dst, src []byte) {
			v := float32(src[0])
			binary.LittleEndian.PutUint32(dst, math.Float32bits(v))
		},
	},
	types.PCMFormatS16LE: {
		types.PCMFormatS16LE: func(dst, src []byte) {
			copy(dst, src)
		},
	},
	types.PCMFormatFloat32LE: {
		types.PCMFormatFloat32LE: func(dst, src []byte) {
			copy(dst, src)
		},
		types.PCMFormatS16LE: func(dst, src []byte) {
			f32 := math.Float32frombits(binary.LittleEndian.Uint32(src)) * 0x8000
			binary.LittleEndian.PutUint16(dst, uint16(f32))
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
			r.inNumAvg = uint(r.inFormat.Channels)
		default:
			return fmt.Errorf("do not know how to convert %d channels to %d", r.inFormat.Channels, r.outFormat.Channels)
		}
	}

	sampleRateAdjust := float64(r.outFormat.SampleRate) / float64(r.inFormat.SampleRate)
	r.outDistanceStep = uint64(float64(distanceStep) / sampleRateAdjust)

	r.convert = pcmSampleConvertMap[r.inFormat.PCMFormat][r.outFormat.PCMFormat]
	if r.convert == nil {
		return fmt.Errorf("unable to get a convert function from %s to %s", r.inFormat.PCMFormat, r.outFormat.PCMFormat)
	}
	return nil
}

func (r *Resampler) Read(p []byte) (int, error) {
	r.locker.Lock()
	defer r.locker.Unlock()
	chunksToRead := uint64(len(p)) / uint64(r.outSampleSize) / uint64(r.outNumRepeat) * uint64(r.inFormat.SampleRate) / uint64(r.outFormat.SampleRate)
	bytesToRead := uint64(chunksToRead) * uint64(r.inSampleSize)
	if cap(r.buffer) < int(bytesToRead) {
		r.buffer = make([]byte, bytesToRead)
	} else {
		r.buffer = r.buffer[:bytesToRead]
	}
	n, err := r.inReader.Read(r.buffer)
	r.buffer = r.buffer[:n]

	if n%int(r.inSampleSize) != 0 {
		return 0, fmt.Errorf("read a number of bytes that is not a multiple of %d: %w", r.inSampleSize, err)
	}
	chunksToWrite := uint64(n) / uint64(r.inSampleSize) / uint64(r.inNumAvg) * uint64(r.outFormat.SampleRate) / uint64(r.inFormat.SampleRate)

	for srcChunkIdx, dstChunkIdx := uint64(0), uint64(0); dstChunkIdx < chunksToWrite; {
		for int64(r.inDistance) < int64(r.outDistance)-int64(r.outDistanceStep) {
			srcChunkIdx++
			r.inDistance += distanceStep
		}

		// TODO: It is assumed that r.inNumAvg == 1 (see `init()`), so we omit
		//       averaging the value, yet. Fix this.
		idxSrc := srcChunkIdx * uint64(r.inSampleSize) * uint64(r.inNumAvg)
		for repeatIdx := uint64(0); repeatIdx < uint64(r.outNumRepeat); repeatIdx++ {
			idxDst := uint64((dstChunkIdx*uint64(r.outNumRepeat) + repeatIdx) * uint64(r.outSampleSize))

			src := r.buffer[idxSrc:]
			dst := p[idxDst:]
			r.convert(dst, src)
		}

		prevIdxDst := uint64(dstChunkIdx) * uint64(r.outNumRepeat) * uint64(r.outSampleSize)
		for r.inDistance > r.outDistance+r.outDistanceStep {
			dstChunkIdx++
			r.outDistance += r.outDistanceStep
			idxDst := uint64(dstChunkIdx) * uint64(r.outNumRepeat) * uint64(r.outSampleSize)
			src := p[prevIdxDst:idxDst]
			dst := p[idxDst:]
			copy(dst, src)
			prevIdxDst = idxDst
		}

		srcChunkIdx++
		r.inDistance += distanceStep

		dstChunkIdx++
		r.outDistance += r.outDistanceStep
	}
	return int(chunksToWrite * uint64(r.outSampleSize) * uint64(r.outNumRepeat)), err
}
