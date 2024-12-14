package safeencoder

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/encoder/libav/safeencoder/process"
)

type processBackend = process.Encoder
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
