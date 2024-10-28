package builtin

import (
	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/streamctl/pkg/audio"
)

func pcmFormatToAudio(libavPCMFormat astiav.SampleFormat) audio.PCMFormat {
	switch libavPCMFormat {
	case astiav.SampleFormatDbl:
		return audio.PCMFormatFloat64LE
	case astiav.SampleFormatDblp:
		return audio.PCMFormatFloat64LE
	case astiav.SampleFormatFlt:
		return audio.PCMFormatFloat32LE
	case astiav.SampleFormatFltp:
		return audio.PCMFormatFloat32LE
	case astiav.SampleFormatNb:
		return audio.PCMFormatUndefined
	case astiav.SampleFormatNone:
		return audio.PCMFormatUndefined
	case astiav.SampleFormatS16:
		return audio.PCMFormatS16LE
	case astiav.SampleFormatS16P:
		return audio.PCMFormatS16LE
	case astiav.SampleFormatS32:
		return audio.PCMFormatS32LE
	case astiav.SampleFormatS32P:
		return audio.PCMFormatS32LE
	case astiav.SampleFormatS64:
		return audio.PCMFormatS64LE
	case astiav.SampleFormatS64P:
		return audio.PCMFormatS64LE
	case astiav.SampleFormatU8:
		return audio.PCMFormatU8
	case astiav.SampleFormatU8P:
		return audio.PCMFormatU8
	}
	return audio.PCMFormatUndefined
}
