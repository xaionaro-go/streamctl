package types

import (
	"time"

	"github.com/xaionaro-go/recoder/libav/recoder/types"
)

type CodecConfig struct {
	CodecName       string
	AveragingPeriod time.Duration
	AverageBitRate  uint64
	CustomOptions   []types.CustomOption
}

type EncoderConfig struct {
	Audio CodecConfig
	Video CodecConfig
}
