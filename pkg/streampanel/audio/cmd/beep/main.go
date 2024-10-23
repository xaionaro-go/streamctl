package main

import (
	"github.com/xaionaro-go/streamctl/pkg/streampanel/audio"
)

func main() {
	a := audio.NewAudio()
	a.PlayChatMessage()
}
