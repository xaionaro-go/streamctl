package astiavlogger

import (
	"fmt"

	"github.com/asticode/go-astiav"
)

func ClassCategoryToString(
	cat astiav.ClassCategory,
) string {
	switch cat {
	case astiav.ClassCategoryBitstreamFilter:
		return "BitstreamFilter"
	case astiav.ClassCategoryDecoder:
		return "Decoder"
	case astiav.ClassCategoryDemuxer:
		return "Demuxer"
	case astiav.ClassCategoryDeviceAudioInput:
		return "DeviceAudioInput"
	case astiav.ClassCategoryDeviceAudioOutput:
		return "DeviceAudioOutput"
	case astiav.ClassCategoryDeviceInput:
		return "DeviceInput"
	case astiav.ClassCategoryDeviceOutput:
		return "DeviceOutput"
	case astiav.ClassCategoryDeviceVideoInput:
		return "DeviceVideoInput"
	case astiav.ClassCategoryDeviceVideoOutput:
		return "DeviceVideoOutput"
	case astiav.ClassCategoryEncoder:
		return "Encoder"
	case astiav.ClassCategoryFilter:
		return "Filter"
	case astiav.ClassCategoryInput:
		return "Input"
	case astiav.ClassCategoryMuxer:
		return "Muxer"
	case astiav.ClassCategoryNa:
		return "Na"
	case astiav.ClassCategoryNb:
		return "Nb"
	case astiav.ClassCategoryOutput:
		return "Output"
	case astiav.ClassCategorySwresampler:
		return "Swresampler"
	case astiav.ClassCategorySwscaler:
		return "Swscaler"
	default:
		return fmt.Sprintf("unexpected_class_category_%d", cat)
	}
}
