package recoder

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
)

type decoderStreamHardware struct {
	codec                 *astiav.Codec
	codecContext          *astiav.CodecContext
	hardwareDeviceContext *astiav.HardwareDeviceContext
	hardwarePixelFormat   astiav.PixelFormat
	inputStream           *astiav.Stream
}

func (d *decoderStreamHardware) CodecContext() *astiav.CodecContext {
	return d.codecContext
}

func (d *decoderStreamHardware) InputStream() *astiav.Stream {
	return d.inputStream
}

func (d *decoderStreamHardware) Close() error {
	d.codecContext.Free()
	return nil
}

func (d *Decoder) newHardwareDecoder(
	ctx context.Context,
	_ *Input,
	stream *astiav.Stream,
) (_ret *decoderStreamHardware, _err error) {
	if stream.CodecParameters().MediaType() != astiav.MediaTypeVideo {
		return nil, fmt.Errorf("currently hardware decoding is supported only for video streams")
	}

	decoder := &decoderStreamHardware{inputStream: stream}
	defer func() {
		if _err != nil {
			_ = decoder.Close()
		}
	}()

	if d.Config.CodecName != "" {
		decoder.codec = astiav.FindDecoderByName(string(d.Config.CodecName))
	} else {
		decoder.codec = astiav.FindDecoder(stream.CodecParameters().CodecID())
	}
	if decoder.codec == nil {
		return nil, fmt.Errorf("unable to find a codec using codec ID %v", stream.CodecParameters().CodecID())
	}

	if decoder.codecContext = astiav.AllocCodecContext(decoder.codec); decoder.codecContext == nil {
		return nil, fmt.Errorf("unable to allocate codec context")
	}

	for _, p := range decoder.codec.HardwareConfigs() {
		if p.MethodFlags().Has(astiav.CodecHardwareConfigMethodFlagHwDeviceCtx) && p.HardwareDeviceType() == d.HardwareDeviceType {
			decoder.hardwarePixelFormat = p.PixelFormat()
			break
		}
	}

	if decoder.hardwarePixelFormat == astiav.PixelFormatNone {
		return nil, fmt.Errorf("hardware device type '%v' is not supported", d.HardwareDeviceType)
	}

	if err := stream.CodecParameters().ToCodecContext(decoder.codecContext); err != nil {
		return nil, fmt.Errorf("CodecParameters().ToCodecContext(...) returned error: %w", err)
	}

	var err error
	decoder.hardwareDeviceContext, err = astiav.CreateHardwareDeviceContext(
		d.HardwareDeviceType,
		string(d.Config.HardwareDeviceTypeName),
		nil,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to create hardware device context: %w", err)
	}

	decoder.codecContext.SetHardwareDeviceContext(decoder.hardwareDeviceContext)
	decoder.codecContext.SetPixelFormatCallback(func(pfs []astiav.PixelFormat) astiav.PixelFormat {
		for _, pf := range pfs {
			if pf == decoder.hardwarePixelFormat {
				return pf
			}
		}

		logger.Errorf(ctx, "unable to find appropriate pixel format")
		return astiav.PixelFormatNone
	})

	// TODO: figure out if we need to do GuessFrameRate here

	if err := decoder.codecContext.Open(decoder.codec, nil); err != nil {
		return nil, fmt.Errorf("unable to open codec context: %w", err)
	}

	return decoder, nil
}
