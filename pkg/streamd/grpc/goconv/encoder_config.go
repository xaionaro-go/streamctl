package goconv

import (
	"fmt"

	"github.com/xaionaro-go/recoder"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
)

func EncoderConfigToThrift(
	enable bool,
	cfg recoder.EncodersConfig,
) *streamd_grpc.EncoderConfig {
	return &streamd_grpc.EncoderConfig{
		Enable:            enable,
		OutputAudioTracks: OutputAudioTracksToThrift(cfg.OutputAudioTracks),
		OutputVideoTracks: OutputVideoTracksToThrift(cfg.OutputVideoTracks),
	}
}

func OutputAudioTracksToThrift(
	tracks []recoder.AudioTrackEncodingConfig,
) []*streamd_grpc.OutputAudioTrack {
	return convertSlice(tracks, OutputAudioTrackToThrift)
}

func OutputAudioTrackToThrift(
	cfg recoder.AudioTrackEncodingConfig,
) *streamd_grpc.OutputAudioTrack {
	return &streamd_grpc.OutputAudioTrack{
		InputTrackIDs: convertSlice(cfg.InputTrackIDs, func(in int) uint64 { return uint64(in) }),
		Encode:        EncodeAudioConfigToThrift(cfg.Config),
	}
}

func EncodeAudioConfigToThrift(
	cfg recoder.EncodeAudioConfig,
) *streamd_grpc.EncodeAudioConfig {
	return &streamd_grpc.EncodeAudioConfig{
		Codec:   AudioCodecToThrift(cfg.Codec),
		Quality: AudioQualityToThrift(cfg.Quality),
	}
}

func AudioCodecToThrift(
	codec recoder.AudioCodec,
) streamd_grpc.AudioCodec {
	switch codec {
	case recoder.AudioCodecCopy:
		return streamd_grpc.AudioCodec_AudioCodecCopy
	case recoder.AudioCodecAAC:
		return streamd_grpc.AudioCodec_AudioCodecAAC
	case recoder.AudioCodecVorbis:
		return streamd_grpc.AudioCodec_AudioCodecVorbis
	case recoder.AudioCodecOpus:
		return streamd_grpc.AudioCodec_AudioCodecOpus
	default:
		panic(fmt.Errorf("unexpected audio codec: '%s'", codec))
	}
}

func AudioQualityToThrift(
	q recoder.AudioQuality,
) *streamd_grpc.AudioQuality {
	if q == nil {
		return nil
	}
	switch q := q.(type) {
	case *recoder.AudioQualityConstantBitrate:
		return &streamd_grpc.AudioQuality{
			AudioQuality: &streamd_grpc.AudioQuality_ConstantBitrate{
				ConstantBitrate: uint32(*q),
			},
		}
	default:
		panic(fmt.Errorf("unexpected audio quality type: '%T' (%v)", q, q))
	}
}

func OutputVideoTracksToThrift(
	tracks []recoder.VideoTrackEncodingConfig,
) []*streamd_grpc.OutputVideoTrack {
	return convertSlice(tracks, OutputVideoTrackToThrift)
}

func OutputVideoTrackToThrift(
	cfg recoder.VideoTrackEncodingConfig,
) *streamd_grpc.OutputVideoTrack {
	return &streamd_grpc.OutputVideoTrack{
		InputTrackIDs: convertSlice(cfg.InputTrackIDs, func(in int) uint64 { return uint64(in) }),
		Encode:        EncodeVideoConfigToThrift(cfg.Config),
	}
}

func EncodeVideoConfigToThrift(
	cfg recoder.EncodeVideoConfig,
) *streamd_grpc.EncodeVideoConfig {
	return &streamd_grpc.EncodeVideoConfig{
		Codec:   VideoCodecToThrift(cfg.Codec),
		Quality: VideoQualityToThrift(cfg.Quality),
	}
}

func VideoCodecToThrift(
	codec recoder.VideoCodec,
) streamd_grpc.VideoCodec {
	switch codec {
	case recoder.VideoCodecCopy:
		return streamd_grpc.VideoCodec_VideoCodecCopy
	case recoder.VideoCodecH264:
		return streamd_grpc.VideoCodec_VideoCodecH264
	case recoder.VideoCodecHEVC:
		return streamd_grpc.VideoCodec_VideoCodecHEVC
	case recoder.VideoCodecAV1:
		return streamd_grpc.VideoCodec_VideoCodecAV1
	default:
		panic(fmt.Errorf("unexpected video codec: '%s'", codec))
	}
}

func VideoQualityToThrift(
	q recoder.VideoQuality,
) *streamd_grpc.VideoQuality {
	if q == nil {
		return nil
	}
	switch q := q.(type) {
	case *recoder.VideoQualityConstantBitrate:
		return &streamd_grpc.VideoQuality{
			VideoQuality: &streamd_grpc.VideoQuality_ConstantBitrate{
				ConstantBitrate: uint32(*q),
			},
		}
	case *recoder.VideoQualityConstantQuality:
		return &streamd_grpc.VideoQuality{
			VideoQuality: &streamd_grpc.VideoQuality_ConstantQuality{
				ConstantQuality: uint32(*q),
			},
		}
	default:
		panic(fmt.Errorf("unexpected video quality type: '%T' (%v)", q, q))
	}
}

func EncoderConfigFromThrift(
	cfg *streamd_grpc.EncoderConfig,
) (recoder.EncodersConfig, bool) {
	if cfg == nil {
		return recoder.EncodersConfig{}, false
	}
	return recoder.EncodersConfig{
		OutputAudioTracks: OutputAudioTracksFromThrift(cfg.OutputAudioTracks),
		OutputVideoTracks: OutputVideoTracksFromThrift(cfg.OutputVideoTracks),
	}, cfg.Enable
}

func OutputAudioTracksFromThrift(
	tracks []*streamd_grpc.OutputAudioTrack,
) []recoder.AudioTrackEncodingConfig {
	return convertSlice(tracks, OutputAudioTrackFromThrift)
}

func OutputAudioTrackFromThrift(
	cfg *streamd_grpc.OutputAudioTrack,
) recoder.AudioTrackEncodingConfig {
	return recoder.AudioTrackEncodingConfig{
		InputTrackIDs: convertSlice(cfg.InputTrackIDs, func(in uint64) int { return int(in) }),
		Config:        EncodeAudioConfigFromThrift(cfg.Encode),
	}
}

func EncodeAudioConfigFromThrift(
	cfg *streamd_grpc.EncodeAudioConfig,
) recoder.EncodeAudioConfig {
	return recoder.EncodeAudioConfig{
		Codec:   AudioCodecFromThrift(cfg.Codec),
		Quality: AudioQualityFromThrift(cfg.Quality),
	}
}

func AudioCodecFromThrift(
	codec streamd_grpc.AudioCodec,
) recoder.AudioCodec {
	switch codec {
	case streamd_grpc.AudioCodec_AudioCodecCopy:
		return recoder.AudioCodecCopy
	case streamd_grpc.AudioCodec_AudioCodecAAC:
		return recoder.AudioCodecAAC
	case streamd_grpc.AudioCodec_AudioCodecVorbis:
		return recoder.AudioCodecVorbis
	case streamd_grpc.AudioCodec_AudioCodecOpus:
		return recoder.AudioCodecOpus
	default:
		panic(fmt.Errorf("unexpected audio codec: '%s'", codec))
	}
}

func AudioQualityFromThrift(
	q *streamd_grpc.AudioQuality,
) recoder.AudioQuality {
	if q == nil {
		return nil
	}
	switch q := q.GetAudioQuality().(type) {
	case *streamd_grpc.AudioQuality_ConstantBitrate:
		return ptr(recoder.AudioQualityConstantBitrate(q.ConstantBitrate))
	default:
		panic(fmt.Errorf("unexpected audio quality type: '%T' (%v)", q, q))
	}
}

func OutputVideoTracksFromThrift(
	tracks []*streamd_grpc.OutputVideoTrack,
) []recoder.VideoTrackEncodingConfig {
	return convertSlice(tracks, OutputVideoTrackFromThrift)
}

func OutputVideoTrackFromThrift(
	cfg *streamd_grpc.OutputVideoTrack,
) recoder.VideoTrackEncodingConfig {
	return recoder.VideoTrackEncodingConfig{
		InputTrackIDs: convertSlice(cfg.InputTrackIDs, func(in uint64) int { return int(in) }),
		Config:        EncodeVideoConfigFromThrift(cfg.Encode),
	}
}

func EncodeVideoConfigFromThrift(
	cfg *streamd_grpc.EncodeVideoConfig,
) recoder.EncodeVideoConfig {
	return recoder.EncodeVideoConfig{
		Codec:   VideoCodecFromThrift(cfg.Codec),
		Quality: VideoQualityFromThrift(cfg.Quality),
	}
}

func VideoCodecFromThrift(
	codec streamd_grpc.VideoCodec,
) recoder.VideoCodec {
	switch codec {
	case streamd_grpc.VideoCodec_VideoCodecCopy:
		return recoder.VideoCodecCopy
	case streamd_grpc.VideoCodec_VideoCodecH264:
		return recoder.VideoCodecH264
	case streamd_grpc.VideoCodec_VideoCodecHEVC:
		return recoder.VideoCodecHEVC
	case streamd_grpc.VideoCodec_VideoCodecAV1:
		return recoder.VideoCodecAV1
	default:
		panic(fmt.Errorf("unexpected video codec: '%s'", codec))
	}
}

func VideoQualityFromThrift(
	q *streamd_grpc.VideoQuality,
) recoder.VideoQuality {
	if q == nil {
		return nil
	}
	switch q := q.GetVideoQuality().(type) {
	case *streamd_grpc.VideoQuality_ConstantBitrate:
		return ptr(recoder.VideoQualityConstantBitrate(q.ConstantBitrate))
	case *streamd_grpc.VideoQuality_ConstantQuality:
		return ptr(recoder.VideoQualityConstantQuality(q.ConstantQuality))
	default:
		panic(fmt.Errorf("unexpected video quality type: '%T' (%v)", q, q))
	}
}
