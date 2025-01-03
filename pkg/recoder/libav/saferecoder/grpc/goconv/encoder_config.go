package goconv

import (
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/recoder/libav/saferecoder/grpc/go/recoder_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

func EncoderConfigToThrift(
	cfg streamtypes.VideoConvertConfig,
) *recoder_grpc.EncoderConfig {
	return &recoder_grpc.EncoderConfig{
		OutputAudioTracks: OutputAudioTracksToThrift(cfg.OutputAudioTracks),
		OutputVideoTracks: OutputVideoTracksToThrift(cfg.OutputVideoTracks),
	}
}

func convertSlice[IN, OUT any](
	tracks []IN,
	convFunc func(IN) OUT,
) []OUT {
	result := make([]OUT, 0, len(tracks))
	for _, item := range tracks {
		result = append(result, convFunc(item))
	}
	return result
}

func OutputAudioTracksToThrift(
	tracks []streamtypes.AudioTrackConfig,
) []*recoder_grpc.OutputAudioTrack {
	return convertSlice(tracks, OutputAudioTrackToThrift)
}

func OutputAudioTrackToThrift(
	cfg streamtypes.AudioTrackConfig,
) *recoder_grpc.OutputAudioTrack {
	return &recoder_grpc.OutputAudioTrack{
		InputID:       uint64(cfg.InputID),
		InputTrackIDs: convertSlice(cfg.InputTrackIDs, func(in int) uint64 { return uint64(in) }),
		Encode:        EncodeAudioConfigToThrift(cfg.EncodeAudioConfig),
	}
}

func EncodeAudioConfigToThrift(
	cfg streamtypes.EncodeAudioConfig,
) *recoder_grpc.EncodeAudioConfig {
	return &recoder_grpc.EncodeAudioConfig{
		Codec:   AudioCodecToThrift(cfg.Codec),
		Quality: AudioQualityToThrift(cfg.Quality),
	}
}

func AudioCodecToThrift(
	codec streamtypes.AudioCodec,
) recoder_grpc.AudioCodec {
	switch codec {
	case streamtypes.AudioCodecAAC:
		return recoder_grpc.AudioCodec_AudioCodecAAC
	case streamtypes.AudioCodecVorbis:
		return recoder_grpc.AudioCodec_AudioCodecVorbis
	case streamtypes.AudioCodecOpus:
		return recoder_grpc.AudioCodec_AudioCodecOpus
	default:
		panic(fmt.Errorf("unexpected codec: '%v'", codec))
	}
}

func AudioQualityToThrift(
	q streamtypes.AudioQuality,
) *recoder_grpc.AudioQuality {
	switch q := q.(type) {
	case *streamtypes.AudioQualityConstantBitrate:
		return &recoder_grpc.AudioQuality{
			AudioQuality: &recoder_grpc.AudioQuality_ConstantBitrate{
				ConstantBitrate: uint32(*q),
			},
		}
	default:
		panic(fmt.Errorf("unexpected audio quality type: '%T' (%v)", q, q))
	}
}

func OutputVideoTracksToThrift(
	tracks []streamtypes.VideoTrackConfig,
) []*recoder_grpc.OutputVideoTrack {
	return convertSlice(tracks, OutputVideoTrackToThrift)
}

func OutputVideoTrackToThrift(
	cfg streamtypes.VideoTrackConfig,
) *recoder_grpc.OutputVideoTrack {
	return &recoder_grpc.OutputVideoTrack{
		InputID:       uint64(cfg.InputID),
		InputTrackIDs: convertSlice(cfg.InputTrackIDs, func(in int) uint64 { return uint64(in) }),
		Encode:        EncodeVideoConfigToThrift(cfg.EncodeVideoConfig),
	}
}

func EncodeVideoConfigToThrift(
	cfg streamtypes.EncodeVideoConfig,
) *recoder_grpc.EncodeVideoConfig {
	return &recoder_grpc.EncodeVideoConfig{
		Codec:   VideoCodecToThrift(cfg.Codec),
		Quality: VideoQualityToThrift(cfg.Quality),
	}
}

func VideoCodecToThrift(
	codec streamtypes.VideoCodec,
) recoder_grpc.VideoCodec {
	switch codec {
	case streamtypes.VideoCodecH264:
		return recoder_grpc.VideoCodec_VideoCodecH264
	case streamtypes.VideoCodecHEVC:
		return recoder_grpc.VideoCodec_VideoCodecHEVC
	case streamtypes.VideoCodecAV1:
		return recoder_grpc.VideoCodec_VideoCodecAV1
	default:
		panic(fmt.Errorf("unexpected codec: '%v'", codec))
	}
}

func VideoQualityToThrift(
	q streamtypes.VideoQuality,
) *recoder_grpc.VideoQuality {
	switch q := q.(type) {
	case *streamtypes.VideoQualityConstantBitrate:
		return &recoder_grpc.VideoQuality{
			VideoQuality: &recoder_grpc.VideoQuality_ConstantBitrate{
				ConstantBitrate: uint32(*q),
			},
		}
	case *streamtypes.VideoQualityConstantQuality:
		return &recoder_grpc.VideoQuality{
			VideoQuality: &recoder_grpc.VideoQuality_ConstantQuality{
				ConstantQuality: uint32(*q),
			},
		}
	default:
		panic(fmt.Errorf("unexpected video quality type: '%T' (%v)", q, q))
	}
}
