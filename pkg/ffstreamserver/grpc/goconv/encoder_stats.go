package goconv

import (
	"github.com/xaionaro-go/streamctl/pkg/ffstreamserver/grpc/go/ffstream_grpc"
	"github.com/xaionaro-go/streamctl/pkg/recoder/libav/recoder"
)

func EncoderStatsToGRPC(
	req *recoder.CommonsEncoderStatistics,
) *ffstream_grpc.GetEncoderStatsReply {
	return &ffstream_grpc.GetEncoderStatsReply{
		BytesCountRead:              req.BytesCountRead.Load(),
		BytesCountWrote:             req.BytesCountWrote.Load(),
		FramesReadUnparsed:          req.FramesRead.Unparsed.Load(),
		FramesReadVideoUnprocessed:  req.FramesRead.VideoUnprocessed.Load(),
		FramesReadAudioUnprocessed:  req.FramesRead.AudioUnprocessed.Load(),
		FramesReadVideoProcessed:    req.FramesRead.VideoProcessed.Load(),
		FramesReadAudioProcessed:    req.FramesRead.AudioProcessed.Load(),
		FramesWroteUnparsed:         req.FramesWrote.Unparsed.Load(),
		FramesWroteVideoUnprocessed: req.FramesWrote.VideoUnprocessed.Load(),
		FramesWroteAudioUnprocessed: req.FramesWrote.AudioUnprocessed.Load(),
		FramesWroteVideoProcessed:   req.FramesWrote.VideoProcessed.Load(),
		FramesWroteAudioProcessed:   req.FramesWrote.AudioProcessed.Load(),
	}
}
