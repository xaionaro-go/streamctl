package audio

import (
	"bytes"
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	audioSubsystem "github.com/xaionaro-go/audio/pkg/audio"
	_ "github.com/xaionaro-go/audio/pkg/audio/backends/oto"
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
	stream, err := a.Playbacker.PlayVorbis(bytes.NewReader(a.AudioTheme.ChatMessage))
	if err != nil {
		return fmt.Errorf("unable to start playback the sound: %w", err)
	}

	if err := stream.Drain(); err != nil {
		return fmt.Errorf("unable to drain the sound: %w", err)
	}

	if err := stream.Close(); err != nil {
		logger.Errorf(context.TODO(), "unable to close the stream: %v", err)
	}
	return nil
}
