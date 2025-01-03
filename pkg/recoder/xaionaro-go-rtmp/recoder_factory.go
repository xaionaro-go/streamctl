package xaionarogortmp

import (
	"github.com/xaionaro-go/streamctl/pkg/recoder"
)

type RecoderFactory struct{}

var _ recoder.Factory = (*RecoderFactory)(nil)

func NewRecoderFactory() *RecoderFactory {
	return &RecoderFactory{}
}
