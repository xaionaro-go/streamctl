package livego

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/encoder"
)

type EncoderFactory struct{}

var _ recoder.Factory = (*EncoderFactory)(nil)

func NewEncoderFactory() *EncoderFactory {
	return &EncoderFactory{}
}

func (EncoderFactory) New(ctx context.Context, cfg recoder.Config) (recoder.Encoder, error) {
	return &Encoder{}, nil
}
