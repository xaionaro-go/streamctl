package livego

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/recoder"
)

type RecoderFactory struct{}

var _ recoder.Factory = (*RecoderFactory)(nil)

func NewRecoderFactory() *RecoderFactory {
	return &RecoderFactory{}
}

func (RecoderFactory) New(ctx context.Context, cfg recoder.Config) (recoder.Recoder, error) {
	return &Recoder{}, nil
}
