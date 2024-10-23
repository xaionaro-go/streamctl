package main

import (
	"bytes"
	_ "embed"

	"github.com/xaionaro-go/streamctl/pkg/audio"
)

//go:embed resources/long_audio.ogg
var longVorbis []byte

func main() {
	a := audio.NewAudioAuto()
	err := a.PlayVorbis(bytes.NewReader(longVorbis))
	if err != nil {
		panic(err)
	}
}
