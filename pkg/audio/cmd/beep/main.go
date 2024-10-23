package main

import (
	"bytes"

	"github.com/xaionaro-go/streamctl/pkg/audio"
	"github.com/xaionaro-go/streamctl/pkg/audiotheme/defaultaudiotheme"
)

func main() {
	audiotheme := defaultaudiotheme.AudioTheme()
	a := audio.NewAudioAuto()
	err := a.PlayVorbis(bytes.NewReader(audiotheme.ChatMessage))
	if err != nil {
		panic(err)
	}
}
