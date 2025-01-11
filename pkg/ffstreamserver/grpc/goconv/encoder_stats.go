package goconv

import (
	"github.com/xaionaro-go/recoder/libav/recoder/types"
	"github.com/xaionaro-go/streamctl/pkg/ffstreamserver/grpc/go/ffstream_grpc"
)

func EncoderStatsToGRPC(
	req *types.EncoderStatistics,
) *ffstream_grpc.GetEncoderStatsReply {
	return &ffstream_grpc.GetEncoderStatsReply{
		BytesCountRead:              req.BytesCountRead,
		BytesCountWrote:             req.BytesCountWrote,
		FramesReadUnparsed:          req.FramesRead.Unparsed,
		FramesReadVideoUnprocessed:  req.FramesRead.VideoUnprocessed,
		FramesReadAudioUnprocessed:  req.FramesRead.AudioUnprocessed,
		FramesReadVideoProcessed:    req.FramesRead.VideoProcessed,
		FramesReadAudioProcessed:    req.FramesRead.AudioProcessed,
		FramesWroteUnparsed:         req.FramesWrote.Unparsed,
		FramesWroteVideoUnprocessed: req.FramesWrote.VideoUnprocessed,
		FramesWroteAudioUnprocessed: req.FramesWrote.AudioUnprocessed,
		FramesWroteVideoProcessed:   req.FramesWrote.VideoProcessed,
		FramesWroteAudioProcessed:   req.FramesWrote.AudioProcessed,
	}
}

func EncoderStatsFromGRPC(
	req *ffstream_grpc.GetEncoderStatsReply,
) *types.EncoderStatistics {
	result := &types.EncoderStatistics{
		BytesCountRead:  req.GetBytesCountRead(),
		BytesCountWrote: req.GetBytesCountWrote(),
		FramesRead: types.EncoderFramesStatistics{
			Unparsed:         req.GetFramesReadUnparsed(),
			VideoUnprocessed: req.GetFramesReadVideoUnprocessed(),
			AudioUnprocessed: req.GetFramesReadAudioUnprocessed(),
			VideoProcessed:   req.GetFramesReadVideoProcessed(),
			AudioProcessed:   req.GetFramesReadAudioProcessed(),
		},
		FramesWrote: types.EncoderFramesStatistics{
			Unparsed:         req.GetFramesWroteUnparsed(),
			VideoUnprocessed: req.GetFramesWroteVideoUnprocessed(),
			AudioUnprocessed: req.GetFramesWroteAudioUnprocessed(),
			VideoProcessed:   req.GetFramesWroteVideoProcessed(),
			AudioProcessed:   req.GetFramesWroteAudioProcessed(),
		},
	}
	return result
}
