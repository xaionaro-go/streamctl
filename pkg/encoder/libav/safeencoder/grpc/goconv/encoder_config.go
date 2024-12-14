package goconv

import (
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/encoder/libav/safeencoder/grpc/go/encoder_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

func EncoderConfigToThrift(
	cfg streamtypes.VideoConvertConfig,
) *encoder_grpc.EncoderConfig {
	return &encoder_grpc.EncoderConfig{
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
) []*encoder_grpc.OutputAudioTrack {
	return convertSlice(tracks, OutputAudioTrackToThrift)
}

func OutputAudioTrackToThrift(
	cfg streamtypes.AudioTrackConfig,
) *encoder_grpc.OutputAudioTrack {
	return &encoder_grpc.OutputAudioTrack{
		InputTrackIDs: convertSlice(cfg.InputAudioTrackIDs, func(in uint) uint64 { return uint64(in) }),
		Encode:        EncodeAudioConfigToThrift(cfg.EncodeAudioConfig),
	}
}

func EncodeAudioConfigToThrift(
	cfg streamtypes.EncodeAudioConfig,
) *encoder_grpc.EncodeAudioConfig {
	return &encoder_grpc.EncodeAudioConfig{
		Codec:   AudioCodecToThrift(cfg.Codec),
		Quality: AudioQualityToThrift(cfg.Quality),
	}
}

func AudioCodecToThrift(
	codec streamtypes.AudioCodec,
) encoder_grpc.AudioCodec {
	switch codec {
	case streamtypes.AudioCodecAAC:
		return encoder_grpc.AudioCodec_AudioCodecAAC
	case streamtypes.AudioCodecVorbis:
		return encoder_grpc.AudioCodec_AudioCodecVorbis
	case streamtypes.AudioCodecOpus:
		return encoder_grpc.AudioCodec_AudioCodecOpus
	default:
		panic(fmt.Errorf("unexpected codec: '%v'", codec))
	}
}

func AudioQualityToThrift(
	q streamtypes.AudioQuality,
) *encoder_grpc.AudioQuality {
	switch q := q.(type) {
	case *streamtypes.AudioQualityConstantBitrate:
		return &encoder_grpc.AudioQuality{
			AudioQuality: &encoder_grpc.AudioQuality_ConstantBitrate{
				ConstantBitrate: uint32(*q),
			},
		}
	default:
		panic(fmt.Errorf("unexpected audio quality type: '%T' (%v)", q, q))
	}
}

func OutputVideoTracksToThrift(
	tracks []streamtypes.VideoTrackConfig,
) []*encoder_grpc.OutputVideoTrack {
	return convertSlice(tracks, OutputVideoTrackToThrift)
}

func OutputVideoTrackToThrift(
	cfg streamtypes.VideoTrackConfig,
) *encoder_grpc.OutputVideoTrack {
	return &encoder_grpc.OutputVideoTrack{
		InputTrackIDs: convertSlice(cfg.InputVideoTrackIDs, func(in uint) uint64 { return uint64(in) }),
		Encode:        EncodeVideoConfigToThrift(cfg.EncodeVideoConfig),
	}
}

func EncodeVideoConfigToThrift(
	cfg streamtypes.EncodeVideoConfig,
) *encoder_grpc.EncodeVideoConfig {
	return &encoder_grpc.EncodeVideoConfig{
		Codec:   VideoCodecToThrift(cfg.Codec),
		Quality: VideoQualityToThrift(cfg.Quality),
	}
}

func VideoCodecToThrift(
	codec streamtypes.VideoCodec,
) encoder_grpc.VideoCodec {
	switch codec {
	case streamtypes.VideoCodecH264:
		return encoder_grpc.VideoCodec_VideoCodecH264
	case streamtypes.VideoCodecHEVC:
		return encoder_grpc.VideoCodec_VideoCodecHEVC
	case streamtypes.VideoCodecAV1:
		return encoder_grpc.VideoCodec_VideoCodecAV1
	default:
		panic(fmt.Errorf("unexpected codec: '%v'", codec))
	}
}

func VideoQualityToThrift(
	q streamtypes.VideoQuality,
) *encoder_grpc.VideoQuality {
	switch q := q.(type) {
	case *streamtypes.VideoQualityConstantBitrate:
		return &encoder_grpc.VideoQuality{
			VideoQuality: &encoder_grpc.VideoQuality_ConstantBitrate{
				ConstantBitrate: uint32(*q),
			},
		}
	case *streamtypes.VideoQualityConstantQuality:
		return &encoder_grpc.VideoQuality{
			VideoQuality: &encoder_grpc.VideoQuality_ConstantQuality{
				ConstantQuality: uint32(*q),
			},
		}
	default:
		panic(fmt.Errorf("unexpected video quality type: '%T' (%v)", q, q))
	}
}
