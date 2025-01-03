package saferecoder

import (
	"context"
	"fmt"
	"runtime"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/recoder/libav/saferecoder/process"
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

	p := &Process{
		processBackend: recoderProcess,
	}
	runtime.SetFinalizer(p, func(p *Process) {
		err := p.Kill()
		logger.Debugf(ctx, "kill result: %v", err)
	})
	return p, nil
}
