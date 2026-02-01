package goconv

import (
	"time"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

func AccountIDFullyQualifiedFromGRPC(in *streamd_grpc.AccountIDFullyQualified) streamcontrol.AccountIDFullyQualified {
	if in == nil {
		return streamcontrol.AccountIDFullyQualified{}
	}
	return streamcontrol.NewAccountIDFullyQualified(
		streamcontrol.PlatformID(in.GetPlatformID()),
		streamcontrol.AccountID(in.GetAccountID()),
	)
}

func AccountIDFullyQualifiedToGRPC(in streamcontrol.AccountIDFullyQualified) *streamd_grpc.AccountIDFullyQualified {
	return &streamd_grpc.AccountIDFullyQualified{
		PlatformID: string(in.PlatformID),
		AccountID:  string(in.AccountID),
	}
}

func StreamSinkTypeGo2GRPC(in streamtypes.StreamSinkType) streamd_grpc.StreamSinkType {
	switch in {
	case streamtypes.StreamSinkTypeCustom:
		return streamd_grpc.StreamSinkType_StreamSinkTypeCustom
	case streamtypes.StreamSinkTypeLocal:
		return streamd_grpc.StreamSinkType_StreamSinkTypeLoopback
	case streamtypes.StreamSinkTypeExternalPlatform:
		return streamd_grpc.StreamSinkType_StreamSinkTypeExternalPlatform
	}
	return streamd_grpc.StreamSinkType_UndefinedStreamSinkType
}

func StreamSinkTypeGRPC2Go(in streamd_grpc.StreamSinkType) streamtypes.StreamSinkType {
	switch in {
	case streamd_grpc.StreamSinkType_StreamSinkTypeCustom:
		return streamtypes.StreamSinkTypeCustom
	case streamd_grpc.StreamSinkType_StreamSinkTypeLoopback:
		return streamtypes.StreamSinkTypeLocal
	case streamd_grpc.StreamSinkType_StreamSinkTypeExternalPlatform:
		return streamtypes.StreamSinkTypeExternalPlatform
	}
	return streamtypes.UndefinedStreamSinkType
}

func StreamSinkIDFullyQualifiedFromGRPC(in *streamd_grpc.StreamSinkIDFullyQualified) streamtypes.StreamSinkIDFullyQualified {
	if in == nil {
		return streamtypes.StreamSinkIDFullyQualified{}
	}
	return streamtypes.NewStreamSinkIDFullyQualified(
		StreamSinkTypeGRPC2Go(in.GetType()),
		streamtypes.StreamSinkID(in.GetStreamSinkID()),
	)
}

func StreamSinkIDFullyQualifiedToGRPC(in streamtypes.StreamSinkIDFullyQualified) *streamd_grpc.StreamSinkIDFullyQualified {
	return &streamd_grpc.StreamSinkIDFullyQualified{
		Type:         StreamSinkTypeGo2GRPC(in.Type),
		StreamSinkID: string(in.ID),
	}
}

func StreamIDFullyQualifiedFromGRPC(in *streamd_grpc.StreamIDFullyQualified) streamcontrol.StreamIDFullyQualified {
	if in == nil || in.GetPlatformID() == "" || in.GetAccountID() == "" || in.GetStreamID() == "" {
		return streamcontrol.StreamIDFullyQualified{}
	}
	return streamcontrol.NewStreamIDFullyQualified(
		streamcontrol.PlatformID(in.GetPlatformID()),
		streamcontrol.AccountID(in.GetAccountID()),
		streamcontrol.StreamID(in.GetStreamID()),
	)
}

func StreamIDFullyQualifiedToGRPC(in streamcontrol.StreamIDFullyQualified) *streamd_grpc.StreamIDFullyQualified {
	return &streamd_grpc.StreamIDFullyQualified{
		PlatformID: string(in.PlatformID),
		AccountID:  string(in.AccountID),
		StreamID:   string(in.StreamID),
	}
}

func secsGRPC2Go(secs float64) time.Duration {
	return time.Duration(float64(time.Second) * secs)
}
