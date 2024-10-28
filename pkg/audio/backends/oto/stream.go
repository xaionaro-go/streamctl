package oto

import (
	"time"

	"github.com/ebitengine/oto/v3"
	"github.com/xaionaro-go/streamctl/pkg/audio/types"
)

type Stream struct {
	*oto.Player
}

var _ types.Stream = (*Stream)(nil)

func newStream(otoPlayer *oto.Player) *Stream {
	return &Stream{
		Player: otoPlayer,
	}
}

func (stream *Stream) Drain() error {
	for stream.Player.IsPlaying() {
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

func (stream *Stream) Close() error {
	return stream.Player.Close()
}
