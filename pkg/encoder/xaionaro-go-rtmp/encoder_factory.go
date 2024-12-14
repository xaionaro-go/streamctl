package xaionarogortmp

import "github.com/xaionaro-go/streamctl/pkg/encoder"

type EncoderFactory struct{}

var _ encoder.Factory = (*EncoderFactory)(nil)

func NewEncoderFactory() *EncoderFactory {
	return &EncoderFactory{}
}
