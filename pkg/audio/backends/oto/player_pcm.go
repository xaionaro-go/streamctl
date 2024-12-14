package oto

import (
	"fmt"
	"io"
	"time"

	"github.com/xaionaro-go/streamctl/pkg/audio/resampler"
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
	sampleRate types.SampleRate,
	channels types.Channel,
	format types.PCMFormat,
	bufferSize time.Duration,
	reader io.Reader,
) (types.Stream, error) {
	// Unfortunately, `oto` does not allow to initialize a context multiple times, so we cannot change the context every time different sampleRate, channels, format or bufferSize are given.
	// As a result, we've just chosen reasonable values and expect them always :(
	if bufferSize != BufferSize {
		return nil, fmt.Errorf("expected buffer size is %v, but received a request for %v", BufferSize, bufferSize)
	}
	if sampleRate != SampleRate || channels != Channels || format != Format {
		inFmt := resampler.Format{
			Channels:   channels,
			SampleRate: sampleRate,
			PCMFormat:  format,
		}
		outFmt := resampler.Format{
			Channels:   Channels,
			SampleRate: SampleRate,
			PCMFormat:  Format,
		}
		var err error
		reader, err = resampler.NewResampler(inFmt, reader, outFmt)
		if err != nil {
			return nil, fmt.Errorf("unable to initialize a resampler from %#+v to %#+v: %w", inFmt, outFmt, err)
		}
	}

	otoCtx, err := getOtoContext()
	if err != nil {
		return nil, fmt.Errorf("unable to get an oto context: %w", err)
	}

	player := otoCtx.NewPlayer(reader)
	player.Play()

	return newStream(player), nil
}
