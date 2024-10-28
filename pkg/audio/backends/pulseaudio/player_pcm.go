package pulseaudio

import (
	"fmt"
	"io"
	"time"

	"github.com/jfreymuth/pulse"
	"github.com/jfreymuth/pulse/proto"
	"github.com/xaionaro-go/streamctl/pkg/audio/types"
)

type PlayerPCM struct {
}

var _ types.PlayerPCM = (*PlayerPCM)(nil)

func NewPlayerPCM() PlayerPCM {
	return PlayerPCM{}
}

func (PlayerPCM) Ping() error {
	c, err := pulse.NewClient()
	if err != nil {
		return fmt.Errorf("unable to open a client to Pulse: %w", err)
	}
	defer c.Close()
	return nil
}

func (PlayerPCM) PlayPCM(
	sampleRate uint32,
	channels uint16,
	format types.PCMFormat,
	bufferSize time.Duration,
	rawReader io.Reader,
) (types.Stream, error) {
	reader, err := newPulseReader(format, rawReader)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize a reader for Pulse: %w", err)
	}

	c, err := pulse.NewClient()
	if err != nil {
		return nil, fmt.Errorf("unable to open a client to Pulse: %w", err)
	}
	defer c.Close()

	stream, err := c.NewPlayback(reader, pulse.PlaybackLatency(bufferSize.Seconds()))
	if err != nil {
		return nil, fmt.Errorf("unable to initialize a playback: %w", err)
	}

	stream.Start()
	stream.Drain()
	if stream.Error() != nil {
		return nil, fmt.Errorf("an error occurred during playback: %w", stream.Error())
	}
	if stream.Underflow() {
		return nil, fmt.Errorf("underflow")
	}
	func() {
		defer func() {
			r := recover()
			if r != nil {
				err = fmt.Errorf("got a panic: %v", r)
			}
		}()
		stream.Close()
	}()
	if err != nil {
		return nil, fmt.Errorf("unable to close the stream: %w", err)
	}

	return newStream(stream), nil
}

type pulseReader struct {
	pulseFormat byte
	io.Reader
}

func newPulseReader(pcmFormat types.PCMFormat, reader io.Reader) (*pulseReader, error) {
	var pulseFormat byte
	switch pcmFormat {
	case types.PCMFormatFloat32LE:
		pulseFormat = proto.FormatFloat32LE
	default:
		return nil, fmt.Errorf("received an unexpected format: %v", pcmFormat)
	}
	return &pulseReader{
		pulseFormat: pulseFormat,
		Reader:      reader,
	}, nil
}

var _ pulse.Reader = (*pulseReader)(nil)

func (r pulseReader) Format() byte {
	return r.pulseFormat
}
