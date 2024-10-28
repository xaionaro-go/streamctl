package audio

import (
	"io"
	"time"
)

type PlayerPCMDummy struct{}

var _ PlayerPCM = PlayerPCMDummy{}

func (PlayerPCMDummy) Ping() error {
	return nil
}

func (PlayerPCMDummy) PlayPCM(
	sampleRate uint32,
	channels uint16,
	format PCMFormat,
	bufferSize time.Duration,
	reader io.Reader,
) (Stream, error) {
	return StreamDummy{}, nil
}

type StreamDummy struct{}

var _ Stream = StreamDummy{}

func (StreamDummy) Drain() error {
	return nil
}

func (StreamDummy) Close() error {
	return nil
}
