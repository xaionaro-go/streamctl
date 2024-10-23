package audio

import (
	"fmt"
	"io"
	"time"

	"github.com/ebitengine/oto/v3"
)

type PlayerOto struct {
}

var _ PlayerPCM = (*PlayerOto)(nil)

func NewPlayerOto() PlayerPCM {
	return PlayerOto{}
}

func (PlayerOto) Ping() error {
	return fmt.Errorf("not implemented: do not know how to do that, yet")
}

func (PlayerOto) PlayPCM(
	sampleRate uint32,
	channels uint16,
	format PCMFormat,
	bufferSize time.Duration,
	reader io.Reader,
) error {
	op := &oto.NewContextOptions{
		SampleRate:   int(sampleRate),
		ChannelCount: int(channels),
		Format:       oto.FormatFloat32LE,
		BufferSize:   bufferSize,
	}

	otoCtx, readyChan, err := oto.NewContext(op)
	if err != nil {
		return fmt.Errorf("unable to initialize an oto context: %w", err)
	}
	<-readyChan

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
