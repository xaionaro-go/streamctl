package recoder

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/asticode/go-astiav"
)

type EncoderInput struct {
	Input  *Input
	Packet *astiav.Packet
}

type Encoder interface {
	Encode(
		ctx context.Context,
		input EncoderInput,
	) (*EncoderOutput, error)
}

type EncoderOutput struct {
	*astiav.Packet
	OverrideFreeFunc func()
	refCounter       sync.WaitGroup
}

func (o *EncoderOutput) Free() {
	if o.OverrideFreeFunc != nil {
		o.OverrideFreeFunc()
		return
	}

	o.Packet.Free()
}

type CommonsEncoder struct {
	BytesCountRead  atomic.Uint64
	BytesCountWrote atomic.Uint64
}
