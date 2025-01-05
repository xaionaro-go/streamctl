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

func CustomOptionsToGRPC(
	opts []recoder.CustomOption,
) []*ffstream_grpc.CustomOption {
	result := make([]*ffstream_grpc.CustomOption, 0, len(opts))

	for _, opt := range opts {
		result = append(result, &ffstream_grpc.CustomOption{
			Key:   opt.Key,
			Value: opt.Value,
		})
	}

	return result
}
