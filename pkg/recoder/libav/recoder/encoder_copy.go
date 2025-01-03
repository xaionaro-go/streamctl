package recoder

import (
	"context"
	"sync"

	"github.com/asticode/go-astiav"
)

type EncoderCopy struct {
	CommonsEncoder

	Locker       sync.Mutex
	InputStreams map[int]*astiav.Stream
}

func NewEncoderCopy() *EncoderCopy {
	return &EncoderCopy{
		InputStreams: map[int]*astiav.Stream{},
	}
}

func (e *EncoderCopy) Encode(
	ctx context.Context,
	input EncoderInput,
) (*EncoderOutput, error) {
	e.BytesCountRead.Add(uint64(input.Packet.Size()))
	e.BytesCountWrote.Add(uint64(input.Packet.Size()))
	return &EncoderOutput{
		Packet:           input.Packet,
		OverrideFreeFunc: func() {},
	}, nil
}
