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

func EncoderStatsFromGRPC(
	req *ffstream_grpc.GetEncoderStatsReply,
) *recoder.CommonsEncoderStatistics {
	result := &recoder.CommonsEncoderStatistics{}
	result.BytesCountRead.Store(req.BytesCountRead)
	result.BytesCountWrote.Store(req.BytesCountWrote)
	result.FramesRead.Unparsed.Store(req.FramesReadUnparsed)
	result.FramesRead.VideoUnprocessed.Store(req.FramesReadVideoUnprocessed)
	result.FramesRead.AudioUnprocessed.Store(req.FramesReadAudioUnprocessed)
	result.FramesRead.VideoProcessed.Store(req.FramesReadVideoProcessed)
	result.FramesRead.AudioProcessed.Store(req.FramesReadAudioProcessed)
	result.FramesWrote.Unparsed.Store(req.FramesWroteUnparsed)
	result.FramesWrote.VideoUnprocessed.Store(req.FramesWroteVideoUnprocessed)
	result.FramesWrote.AudioUnprocessed.Store(req.FramesWroteAudioUnprocessed)
	result.FramesWrote.VideoProcessed.Store(req.FramesWroteVideoProcessed)
	result.FramesWrote.AudioProcessed.Store(req.FramesWroteAudioProcessed)
	return result
}
