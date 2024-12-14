package encoder

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/encoder"
	"github.com/xaionaro-go/streamctl/pkg/encoder/libav/encoder/types"
	"github.com/xaionaro-go/streamctl/pkg/observability"
)

type EncoderConfig = encoder.Config
type Packet = types.Packet

type EncoderStats struct {
	BytesCountRead  atomic.Uint64
	BytesCountWrote atomic.Uint64
}

type Encoder struct {
	WaiterChan chan struct{}
	Result     error
	EncoderConfig
	EncoderStats
}

func New(
	cfg EncoderConfig,
) *Encoder {
	result := &Encoder{
		WaiterChan:    make(chan struct{}),
		EncoderConfig: cfg,
	}
	close(
		result.WaiterChan,
	) // to prevent Wait() from blocking when the process is not started, yet.
	return result
}

func (r *Encoder) StartRecoding(
	ctx context.Context,
	input *Input,
	output *Output,
) error {
	inputStreams := make(map[int]*astiav.Stream)
	outputStreams := make(map[int]*astiav.Stream)
	for _, inputStream := range input.FormatContext.Streams() {
		if inputStream.CodecParameters().MediaType() != astiav.MediaTypeAudio &&
			inputStream.CodecParameters().MediaType() != astiav.MediaTypeVideo {
			continue
		}
		inputStreams[inputStream.Index()] = inputStream

		outputStream := output.FormatContext.NewStream(nil)
		if outputStream == nil {
			return fmt.Errorf("the output stream is nil")
		}

		if err := inputStream.CodecParameters().Copy(outputStream.CodecParameters()); err != nil {
			return fmt.Errorf("unable to copy the codec parameters: %w", err)
		}

		outputStream.CodecParameters().SetCodecTag(0)
		outputStreams[inputStream.Index()] = outputStream
	}

	if err := output.FormatContext.WriteHeader(nil); err != nil {
		return fmt.Errorf("unable to write the header to the output: %w", err)
	}

	r.WaiterChan = make(chan struct{})
	setResultingError := func(err error) {
		logger.Debugf(ctx, "setResultingError(%v)", err)
		r.Result = err
		close(r.WaiterChan)
	}
	observability.Go(ctx, func() {
		packet := astiav.AllocPacket()
		defer packet.Free()

		for {
			select {
			case <-ctx.Done():
				setResultingError(ctx.Err())
				return
			default:
			}

			// Read frame
			if err := input.FormatContext.ReadFrame(packet); err != nil {
				if errors.Is(err, astiav.ErrEof) {
					break
				}
				setResultingError(fmt.Errorf("unable to read a frame: %w", err))
				return
			}
			logger.Tracef(ctx, "received a frame: %#+v", packet)

			inputStream, ok := inputStreams[packet.StreamIndex()]
			if !ok {
				packet.Unref()
				continue
			}

			outputStream, ok := outputStreams[packet.StreamIndex()]
			if !ok {
				packet.Unref()
				continue
			}

			r.BytesCountRead.Add(uint64(packet.Size()))

			packet.SetStreamIndex(outputStream.Index())
			packet.RescaleTs(inputStream.TimeBase(), outputStream.TimeBase())
			packet.SetPos(-1)

			if err := output.FormatContext.WriteInterleavedFrame(packet); err != nil {
				setResultingError(fmt.Errorf("unable to write the frame: %w", err))
				return
			}

			r.BytesCountWrote.Add(uint64(packet.Size()))
		}

		if err := output.FormatContext.WriteTrailer(); err != nil {
			setResultingError(fmt.Errorf("unable to write the trailer: %w", err))
			return
		}

		logger.Debugf(ctx, "finished re-encoding")
		setResultingError(nil)
	})

	return nil
}

func (r *Encoder) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.WaiterChan:
	}
	return r.Result
}

func (r *Encoder) Recode(
	ctx context.Context,
	input *Input,
	output *Output,
) error {
	err := r.StartRecoding(ctx, input, output)
	if err != nil {
		return fmt.Errorf("got an error while starting the recording: %w", err)
	}

	if err != r.Wait(ctx) {
		return fmt.Errorf("got an error while waiting for a completion: %w", err)
	}

	return nil
}
