package audio

import (
	"fmt"
	"io"
	"time"

	"github.com/jfreymuth/oggvorbis"
)

const BufferSize = 20 * time.Second

type Audio struct {
	PlayerPCM
}

func NewAudio(playerPCM PlayerPCM) *Audio {
	return &Audio{
		PlayerPCM: playerPCM,
	}
}

func NewAudioAuto() *Audio {
	for _, factory := range []func() PlayerPCM{
		NewPlayerPulse,
		NewPlayerOto,
	} {
		player := factory()
		if player.Ping() == nil {
			return &Audio{
				PlayerPCM: player,
			}
		}
	}

	// the default backend:
	return &Audio{
		PlayerPCM: NewPlayerOto(),
	}
}

func (a *Audio) PlayVorbis(rawReader io.Reader) error {
	oggReader, err := oggvorbis.NewReader(rawReader)
	if err != nil {
		return fmt.Errorf("unable to initialize a vorbis reader: %w", err)
	}

	err = a.PlayerPCM.PlayPCM(
		uint32(oggReader.SampleRate()),
		uint16(oggReader.Channels()),
		PCMFormatFloat32LE,
		BufferSize,
		newReaderFromFloat32Reader(oggReader),
	)
	if err != nil {
		return fmt.Errorf("unable to playback as PCM: %w", err)
	}
	return nil
}
