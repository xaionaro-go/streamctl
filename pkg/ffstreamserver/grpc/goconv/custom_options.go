package goconv

import (
	"github.com/xaionaro-go/recoder/libav/recoder/types"
	"github.com/xaionaro-go/streamctl/pkg/ffstreamserver/grpc/go/ffstream_grpc"
)

func CustomOptionsFromGRPC(
	opts []*ffstream_grpc.CustomOption,
) []types.CustomOption {
	result := make([]types.CustomOption, 0, len(opts))

	for _, opt := range opts {
		result = append(result, types.CustomOption{
			Key:   opt.GetKey(),
			Value: opt.GetValue(),
		})
	}

	return result
}

func CustomOptionsToGRPC(
	opts []types.CustomOption,
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
