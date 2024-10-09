package saferecoder

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/streamserver/implementations/libav/recoder/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/implementations/libav/saferecoder/process/client"
)

type InputConfig = types.InputConfig

type InputID = client.InputID
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
