package recoder

import (
	"context"
	"errors"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
)

type RecoderConfig struct{}

type Recorder struct {
	RecoderConfig
}

func New(
	cfg RecoderConfig,
) *Recorder {
	return &Recorder{
		RecoderConfig: cfg,
	}
}

func (r *Recorder) Recode(
	ctx context.Context,
	input *Input,
	output *Output,
) error {
	packet := astiav.AllocPacket()
	defer packet.Free()

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

	for {
		// Read frame
		if err := input.FormatContext.ReadFrame(packet); err != nil {
			if errors.Is(err, astiav.ErrEof) {
				break
			}
			return fmt.Errorf("unable to read a frame: %w", err)
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

		packet.SetStreamIndex(outputStream.Index())
		packet.RescaleTs(inputStream.TimeBase(), outputStream.TimeBase())
		packet.SetPos(-1)

		if err := output.FormatContext.WriteInterleavedFrame(packet); err != nil {
			return fmt.Errorf("unable to write the frame: %w", err)
		}
	}

	if err := output.FormatContext.WriteTrailer(); err != nil {
		return fmt.Errorf("unable to write the trailer: %w", err)
	}

	return nil
}
