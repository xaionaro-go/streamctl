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
) error {
	return nil
}
