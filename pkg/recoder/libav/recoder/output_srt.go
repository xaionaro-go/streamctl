package recoder

import (
	"fmt"

	"github.com/xaionaro-go/libsrt/threadsafe"
)

func (output *Output) SRT() (*threadsafe.Socket, error) {
	if output.FormatContext.OutputFormat().Name() != "srt" {
		return nil, fmt.Errorf("output format is not 'srt'")
	}

	return formatContextToSRTSocket(output.FormatContext)
}
