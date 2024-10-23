package audio

import (
	"bytes"

	audioSubsystem "github.com/xaionaro-go/streamctl/pkg/audio"
	"github.com/xaionaro-go/streamctl/pkg/audiotheme"
	"github.com/xaionaro-go/streamctl/pkg/audiotheme/defaultaudiotheme"
)

type Audio struct {
	playbacker *audioSubsystem.Audio
	audioTheme audiotheme.AudioTheme
}

func NewAudio() *Audio {
	return &Audio{
		playbacker: audioSubsystem.NewAudioAuto(),
		audioTheme: defaultaudiotheme.AudioTheme(),
	}
}

func (a *Audio) PlayChatMessage() error {
	return a.playbacker.PlayVorbis(bytes.NewReader(a.audioTheme.ChatMessage))
}
