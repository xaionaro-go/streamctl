package recoder

import (
	"fmt"

	"github.com/xaionaro-go/libsrt/threadsafe"
)

func (input *Input) SRT() (*threadsafe.Socket, error) {
	if input.FormatContext.InputFormat().Name() != "srt" {
		return nil, fmt.Errorf("input format is not 'srt'")
	}

	return formatContextToSRTSocket(input.FormatContext)
}
