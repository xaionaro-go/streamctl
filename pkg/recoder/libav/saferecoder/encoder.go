package saferecoder

import (
	"context"
)

type Encoder struct {
	Process *Process
	ID      EncoderID
}

func (p *Process) NewEncoder(
	ctx context.Context,
	cfg EncoderConfig,
) (*Encoder, error) {
	encoderID, err := p.Client.NewEncoder(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &Encoder{
		Process: p,
		ID:      encoderID,
	}, nil
}

func (encoder *Encoder) Close() error {
	return encoder.Process.Client.CloseEncoder(context.Background(), encoder.ID)
}
