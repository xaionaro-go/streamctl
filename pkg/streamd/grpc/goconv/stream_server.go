package goconv

import (
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

func StreamServerTypeGo2GRPC(t api.StreamServerType) (streamd_grpc.StreamServerType, error) {
	switch t {
	case streamtypes.ServerTypeUndefined:
		return streamd_grpc.StreamServerType_Undefined, nil
	case streamtypes.ServerTypeRTMP:
		return streamd_grpc.StreamServerType_RTMP, nil
	case streamtypes.ServerTypeRTSP:
		return streamd_grpc.StreamServerType_RTSP, nil
	}
	return streamd_grpc.StreamServerType_Undefined, fmt.Errorf("unexpected value: %v", t)
}

func StreamServerTypeGRPC2Go(t streamd_grpc.StreamServerType) (api.StreamServerType, error) {
	switch t {
	case streamd_grpc.StreamServerType_Undefined:
		return streamtypes.ServerTypeUndefined, nil
	case streamd_grpc.StreamServerType_RTMP:
		return streamtypes.ServerTypeRTMP, nil
	case streamd_grpc.StreamServerType_RTSP:
		return streamtypes.ServerTypeRTSP, nil
	}
	return streamtypes.ServerTypeUndefined, fmt.Errorf("unexpected value: %v", t)
}
