package libav

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/encoder"
	"github.com/xaionaro-go/streamctl/pkg/encoder/libav/safeencoder"
)

type EncoderFactory struct{}

var _ encoder.Factory = (*EncoderFactory)(nil)

func NewEncoderFactory() *EncoderFactory {
	return &EncoderFactory{}
}

func (EncoderFactory) New(
	ctx context.Context,
	cfg encoder.Config,
) (encoder.Encoder, error) {
	process, err := safeencoder.NewProcess(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize the process: %w", err)
	}

	recoderInstance, err := process.NewEncoder(cfg)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize the recoder: %w", err)
	}

	return &Encoder{
		Process: process,
		Encoder: recoderInstance,
	}, nil
}
