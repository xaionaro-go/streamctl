package audio

import (
	"bytes"
	"context"

	audioSubsystem "github.com/xaionaro-go/streamctl/pkg/audio"
	_ "github.com/xaionaro-go/streamctl/pkg/audio/backends/oto"
	"github.com/xaionaro-go/streamctl/pkg/audiotheme"
	"github.com/xaionaro-go/streamctl/pkg/audiotheme/defaultaudiotheme"
)

type Audio struct {
	Playbacker *audioSubsystem.Audio
	AudioTheme audiotheme.AudioTheme
}

func NewAudio(ctx context.Context) *Audio {
	return &Audio{
		Playbacker: audioSubsystem.NewAudioAuto(ctx),
		AudioTheme: defaultaudiotheme.AudioTheme(),
	}
}

func (a *Audio) PlayChatMessage() error {
	return a.Playbacker.PlayVorbis(bytes.NewReader(a.AudioTheme.ChatMessage))
}
