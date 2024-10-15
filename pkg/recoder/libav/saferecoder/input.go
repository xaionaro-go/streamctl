package saferecoder

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/recoder/libav/saferecoder/process"
)

type InputID = process.InputID
type InputConfig = process.InputConfig

type Input struct {
	Process *Process
	ID      InputID
}

func (p *Process) NewInputFromURL(
	ctx context.Context,
	url string,
	cfg InputConfig,
) (*Input, error) {
	inputID, err := p.Client.NewInputFromURL(ctx, url, cfg)
	if err != nil {
		return nil, err
	}
	return &Input{
		Process: p,
		ID:      inputID,
	}, nil
}

func (input *Input) Close() error {
	return input.Process.Client.CloseInput(context.Background(), input.ID)
}
