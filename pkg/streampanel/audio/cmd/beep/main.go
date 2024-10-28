package main

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/streampanel/audio"
)

func main() {
	ctx := context.Background()
	a := audio.NewAudio(ctx)
	a.PlayChatMessage()
}
