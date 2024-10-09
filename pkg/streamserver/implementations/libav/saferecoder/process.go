package saferecoder

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/streamserver/implementations/libav/saferecoder/process"
)

type processBackend = process.Recoder
type Process struct {
	*processBackend
}

func NewProcess(ctx context.Context) (*Process, error) {
	recoderProcess, err := process.Run(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to run the recoder process: %w", err)
	}

	return &Process{
		processBackend: recoderProcess,
	}, nil
}
