package safeencoder

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/encoder/libav/safeencoder/process"
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
	streamKey string,
	cfg OutputConfig,
) (*Output, error) {
	outputID, err := p.Client.NewOutputFromURL(ctx, url, streamKey, cfg)
	if err != nil {
		return nil, err
	}
	return &Output{
		Process: p,
		ID:      outputID,
	}, nil
}

func (output *Output) Close() error {
	return output.Process.Client.CloseOutput(context.Background(), output.ID)
}
