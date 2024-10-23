package audio

import (
	"fmt"
	"io"
	"time"

	"github.com/jfreymuth/pulse"
	"github.com/jfreymuth/pulse/proto"
)

type PlayerPulse struct {
}

var _ PlayerPCM = (*PlayerPulse)(nil)

func NewPlayerPulse() PlayerPCM {
	return PlayerPulse{}
}

func (PlayerPulse) Ping() error {
	c, err := pulse.NewClient()
	if err != nil {
		return fmt.Errorf("unable to open a client to Pulse: %w", err)
	}
	defer c.Close()

	return nil
}

func (PlayerPulse) PlayPCM(
	sampleRate uint32,
	channels uint16,
	format PCMFormat,
	bufferSize time.Duration,
	rawReader io.Reader,
) error {
	reader, err := newPulseReader(format, rawReader)
	if err != nil {
		return fmt.Errorf("unable to initialize a reader for Pulse: %w", err)
	}

	c, err := pulse.NewClient()
	if err != nil {
		return fmt.Errorf("unable to open a client to Pulse: %w", err)
	}
	defer c.Close()

	stream, err := c.NewPlayback(reader, pulse.PlaybackLatency(bufferSize.Seconds()))
	if err != nil {
		return fmt.Errorf("unable to initialize a playback: %w", err)
	}

	stream.Start()
	stream.Drain()
	if stream.Error() != nil {
		return fmt.Errorf("an error occurred during playback: %w", stream.Error())
	}
	if stream.Underflow() {
		return fmt.Errorf("underflow")
	}
	stream.Close()
	return nil
}

type pulseReader struct {
	pulseFormat byte
	io.Reader
}

func newPulseReader(pcmFormat PCMFormat, reader io.Reader) (*pulseReader, error) {
	var pulseFormat byte
	switch pcmFormat {
	case PCMFormatFloat32LE:
		pulseFormat = proto.FormatInt32LE
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
