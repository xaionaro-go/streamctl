package recoder

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
)

type decoderStreamSoftware struct {
	codec        *astiav.Codec
	codecContext *astiav.CodecContext
	inputStream  *astiav.Stream
}

func (d *decoderStreamSoftware) CodecContext() *astiav.CodecContext {
	return d.codecContext
}

func (d *decoderStreamSoftware) InputStream() *astiav.Stream {
	return d.inputStream
}

func (d *decoderStreamSoftware) Close() error {
	d.codecContext.Free()
	return nil
}

func (d *Decoder) newSoftwareDecoder(
	_ context.Context,
	input *Input,
	stream *astiav.Stream,
) (_ret *decoderStreamSoftware, _err error) {
	decoder := &decoderStreamSoftware{
		inputStream: stream,
	}
	defer func() {
		if _err != nil {
			_ = decoder.Close()
		}
	}()

	decoder.codec = astiav.FindDecoder(stream.CodecParameters().CodecID())
	if decoder.codec == nil {
		return nil, fmt.Errorf("unable to find a codec using codec ID %v", stream.CodecParameters().CodecID())
	}

	decoder.codecContext = astiav.AllocCodecContext(decoder.codec)
	if decoder.codecContext == nil {
		return nil, fmt.Errorf("unable to allocate codec context")
	}

	err := stream.CodecParameters().ToCodecContext(decoder.codecContext)
	if err != nil {
		return nil, fmt.Errorf("CodecParameters().ToCodecContext(...) returned error: %w", err)
	}

	if stream.CodecParameters().MediaType() == astiav.MediaTypeVideo {
		decoder.codecContext.SetFramerate(input.FormatContext.GuessFrameRate(stream, nil))
	}

	if err := decoder.codecContext.Open(decoder.codec, nil); err != nil {
		return nil, fmt.Errorf("unable to open codec context: %w", err)
	}

	return decoder, nil
}
