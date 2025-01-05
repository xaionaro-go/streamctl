package recoder

import (
	"context"
	"fmt"
	"sync"

	"github.com/asticode/go-astiav"
)

type EncoderCopy struct {
	CommonsEncoder

	Locker       sync.Mutex
	InputStreams map[int]*astiav.Stream
}

var _ Encoder = (*EncoderCopy)(nil)

func NewEncoderCopy() *EncoderCopy {
	return &EncoderCopy{
		InputStreams: map[int]*astiav.Stream{},
	}
}

func (e *EncoderCopy) Encode(
	ctx context.Context,
	input EncoderInput,
) (_ret *EncoderOutput, _err error) {
	e.BytesCountRead.Add(uint64(input.Packet.Size()))
	defer func() {
		if _ret != nil {
			e.BytesCountWrote.Add(uint64(_ret.Size()))
		}
	}()
	e.Locker.Lock()
	defer e.Locker.Unlock()

	inputStreamIdx := input.Packet.StreamIndex()

	inputStream := e.InputStreams[inputStreamIdx]
	if inputStream == nil {
		for _, stream := range input.Input.Streams() {
			e.InputStreams[stream.Index()] = stream
		}
	}

	inputStream = e.InputStreams[inputStreamIdx]
	if inputStream == nil {
		return nil, fmt.Errorf("unable to find a stream with index #%d", inputStreamIdx)
	}
	return &EncoderOutput{
		Packet: ClonePacketAsWritable(input.Packet),
		Stream: inputStream,
	}, nil
}

func (*EncoderCopy) Close() error {
	return nil
}
