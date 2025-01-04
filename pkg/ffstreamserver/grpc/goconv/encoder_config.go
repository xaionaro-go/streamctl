package goconv

import (
	"github.com/xaionaro-go/streamctl/pkg/ffstream"
	"github.com/xaionaro-go/streamctl/pkg/ffstreamserver/grpc/go/ffstream_grpc"
)

func EncoderConfigFromGRPC(
	req *ffstream_grpc.ConfigureEncoderRequest,
) ffstream.EncoderConfig {
	return ffstream.EncoderConfig{
		Audio: ffstream.CodecConfig{
			CodecName:                       req.GetAudio().GetCodecName(),
			BitRateAveragerBufferSizeInBits: req.GetAudio().GetBitRateAveragerBufferSizeInBits(),
			AverageBitRate:                  req.GetAudio().GetAverageBitRate(),
			CustomOptions:                   CustomOptionsFromGRPC(req.GetAudio().GetCustomOptions()),
		},
		Video: ffstream.CodecConfig{
			CodecName:                       req.GetVideo().GetCodecName(),
			BitRateAveragerBufferSizeInBits: req.GetVideo().GetBitRateAveragerBufferSizeInBits(),
			AverageBitRate:                  req.GetVideo().GetAverageBitRate(),
			CustomOptions:                   CustomOptionsFromGRPC(req.GetVideo().GetCustomOptions()),
		},
	}
}
