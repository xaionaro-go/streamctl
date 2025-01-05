package goconv

import (
	"github.com/xaionaro-go/streamctl/pkg/ffstream/types"
	"github.com/xaionaro-go/streamctl/pkg/ffstreamserver/grpc/go/ffstream_grpc"
)

func EncoderConfigFromGRPC(
	req *ffstream_grpc.EncoderConfig,
) types.EncoderConfig {
	return types.EncoderConfig{
		Audio: types.CodecConfig{
			CodecName:       req.GetAudio().GetCodecName(),
			AveragingPeriod: DurationFromGRPC(int64(req.GetAudio().GetAveragingPeriod())),
			AverageBitRate:  req.GetAudio().GetAverageBitRate(),
			CustomOptions:   CustomOptionsFromGRPC(req.GetAudio().GetCustomOptions()),
		},
		Video: types.CodecConfig{
			CodecName:       req.GetVideo().GetCodecName(),
			AveragingPeriod: DurationFromGRPC(int64(req.GetVideo().GetAveragingPeriod())),
			AverageBitRate:  req.GetVideo().GetAverageBitRate(),
			CustomOptions:   CustomOptionsFromGRPC(req.GetVideo().GetCustomOptions()),
		},
	}
}

func EncoderConfigToGRPC(
	cfg types.EncoderConfig,
) *ffstream_grpc.EncoderConfig {
	return &ffstream_grpc.EncoderConfig{
		Audio: &ffstream_grpc.CodecConfig{
			CodecName:       cfg.Audio.CodecName,
			AveragingPeriod: uint64(DurationToGRPC(cfg.Audio.AveragingPeriod)),
			AverageBitRate:  cfg.Audio.AverageBitRate,
			CustomOptions:   CustomOptionsToGRPC(cfg.Audio.CustomOptions),
		},
		Video: &ffstream_grpc.CodecConfig{
			CodecName:       cfg.Video.CodecName,
			AveragingPeriod: uint64(DurationToGRPC(cfg.Video.AveragingPeriod)),
			AverageBitRate:  cfg.Video.AverageBitRate,
			CustomOptions:   CustomOptionsToGRPC(cfg.Video.CustomOptions),
		},
	}
}
