package goconv

import (
	"github.com/xaionaro-go/streamctl/pkg/ffstreamserver/grpc/go/ffstream_grpc"
	"github.com/xaionaro-go/streamctl/pkg/recoder/libav/recoder"
)

func CustomOptionsFromGRPC(
	opts []*ffstream_grpc.CustomOption,
) []recoder.CustomOption {
	result := make([]recoder.CustomOption, 0, len(opts))

	for _, opt := range opts {
		result = append(result, recoder.CustomOption{
			Key:   opt.GetKey(),
			Value: opt.GetValue(),
		})
	}

	return result
}
