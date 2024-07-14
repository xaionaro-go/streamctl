package grpcconv

import (
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
)

func StreamServerTypeGo2GRPC(t api.StreamServerType) (streamd_grpc.StreamServerType, error) {
	switch t {
	case api.StreamServerTypeUndefined:
		return streamd_grpc.StreamServerType_Undefined, nil
	case api.StreamServerTypeRTMP:
		return streamd_grpc.StreamServerType_RTMP, nil
	case api.StreamServerTypeRTSP:
		return streamd_grpc.StreamServerType_RTSP, nil
	}
	return streamd_grpc.StreamServerType_Undefined, fmt.Errorf("unexpected value: %v", t)
}

func StreamServerTypeGRPC2Go(t streamd_grpc.StreamServerType) (api.StreamServerType, error) {
	switch t {
	case streamd_grpc.StreamServerType_Undefined:
		return api.StreamServerTypeUndefined, nil
	case streamd_grpc.StreamServerType_RTMP:
		return api.StreamServerTypeRTMP, nil
	case streamd_grpc.StreamServerType_RTSP:
		return api.StreamServerTypeRTSP, nil
	}
	return api.StreamServerTypeUndefined, fmt.Errorf("unexpected value: %v", t)
}
