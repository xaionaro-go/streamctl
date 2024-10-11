package saferecoder

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/streamserver/implementations/libav/saferecoder/process"
)

type OutputID = process.OutputID
type OutputConfig = process.OutputConfig

type Output struct {
	Process *Process
	ID      OutputID
}

func (p *Process) NewOutputFromURL(
	ctx context.Context,
	url string,
	cfg OutputConfig,
) (*Output, error) {
	outputID, err := p.Client.NewOutputFromURL(ctx, url, cfg)
	if err != nil {
		return nil, err
	}
	return &Output{
		Process: p,
		ID:      outputID,
	}, nil
}
