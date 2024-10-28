package oto

import (
	"fmt"
	"io"
	"time"

	"github.com/xaionaro-go/streamctl/pkg/audio/types"
)

type PlayerPCM struct {
}

var _ types.PlayerPCM = (*PlayerPCM)(nil)

func NewPlayerPCM() PlayerPCM {
	return PlayerPCM{}
}

func (PlayerPCM) Ping() error {
	// do not know how to do that, yet
	return nil
}

func (PlayerPCM) PlayPCM(
	sampleRate uint32,
	channels uint16,
	format types.PCMFormat,
	bufferSize time.Duration,
	reader io.Reader,
) error {
	// Unfortunately, `oto` does not allow to initialize a context multiple times, so we cannot change the context every time different sampleRate, channels, format or bufferSize are given.
	// As a result, we've just chosen reasonable values and expect them always :(
	if sampleRate != SampleRate {
		return fmt.Errorf("the expected sample rate is %d, but received %d", SampleRate, sampleRate)
	}
	if channels > Channels {
		return fmt.Errorf("the expected number of channels is %d, but received %d", Channels, channels)
	}
	if channels != Channels {
		switch channels {
		case 1:
			reader = repeatReader(reader, uint(format.Size()), Channels)
		default:
			return fmt.Errorf("do not know how to handle %d channels input with %d channels output", channels, Channels)
		}
	}
	if format != Format {
		return fmt.Errorf("the expected format is %v, but received %v", Format, format)
	}
	if bufferSize != BufferSize {
		return fmt.Errorf("the expected buffer size is %v, but received %v", BufferSize, bufferSize)
	}

	otoCtx, err := getOtoContext()
	if err != nil {
		return fmt.Errorf("unable to get an oto context: %w", err)
	}

	player := otoCtx.NewPlayer(reader)
	player.Play()
	for player.IsPlaying() {
		time.Sleep(100 * time.Millisecond)
	}

	err = player.Close()
	if err != nil {
		return fmt.Errorf("unable to close the player: %w", err)
	}

	return nil
}
