package main

import (
	"github.com/xaionaro-go/streamctl/pkg/recoder/libav/recoder"
)

func convertUnknownOptionsToCustomOptions(
	unknownOpts []string,
) []recoder.CustomOption {
	var result []recoder.CustomOption

	for idx := 0; idx < len(unknownOpts)-1; idx++ {
		arg := unknownOpts[idx]

		opt := arg
		value := unknownOpts[idx+1]

		result = append(result, recoder.CustomOption{
			Key:   opt,
			Value: value,
		})
	}

	return result
}
