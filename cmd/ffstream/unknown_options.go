package main

import (
	"fmt"
	"strings"

	"github.com/xaionaro-go/streamctl/pkg/encoder/libav/encoder"
)

func convertUnknownOptionsToCustomOptions(
	unknownOpts []string,
) []encoder.CustomOption {
	var result []encoder.CustomOption

	for idx := 0; idx < len(unknownOpts)-1; idx++ {
		arg := unknownOpts[idx]
		if !strings.HasPrefix(arg, "-") {
			panic(fmt.Errorf("expected an option, and options start with '-', but received '%s'", arg))
		}
		opt := arg[1:]
		value := unknownOpts[idx+1]

		result = append(result, encoder.CustomOption{
			Key:   opt,
			Value: value,
		})
	}

	return result
}
