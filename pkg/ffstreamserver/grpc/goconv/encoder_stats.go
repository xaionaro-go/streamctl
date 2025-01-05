package goconv

import (
	"github.com/xaionaro-go/streamctl/pkg/ffstreamserver/grpc/go/ffstream_grpc"
	"github.com/xaionaro-go/streamctl/pkg/recoder/libav/recoder"
)

func EncoderStatsToGRPC(
	req *recoder.EncoderStatistics,
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
) *recoder.EncoderStatistics {
	result := &recoder.EncoderStatistics{
		BytesCountRead:  req.GetBytesCountRead(),
		BytesCountWrote: req.GetBytesCountWrote(),
		FramesRead: recoder.EncoderFramesStatistics{
			Unparsed:         req.GetFramesReadUnparsed(),
			VideoUnprocessed: req.GetFramesReadVideoUnprocessed(),
			AudioUnprocessed: req.GetFramesReadAudioUnprocessed(),
			VideoProcessed:   req.GetFramesReadVideoProcessed(),
			AudioProcessed:   req.GetFramesReadAudioProcessed(),
		},
		FramesWrote: recoder.EncoderFramesStatistics{
			Unparsed:         req.GetFramesWroteUnparsed(),
			VideoUnprocessed: req.GetFramesWroteVideoUnprocessed(),
			AudioUnprocessed: req.GetFramesWroteAudioUnprocessed(),
			VideoProcessed:   req.GetFramesWroteVideoProcessed(),
			AudioProcessed:   req.GetFramesWroteAudioProcessed(),
		},
	}
	return result
}
