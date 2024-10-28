package pulseaudio

import (
	"fmt"

	"github.com/jfreymuth/pulse"
)

type Stream struct {
	*pulse.PlaybackStream
}

func newStream(pulseStream *pulse.PlaybackStream) *Stream {
	return &Stream{
		PlaybackStream: pulseStream,
	}
}

func (stream *Stream) Drain() error {
	stream.Start()
	if err := stream.Drain(); err != nil {
		return fmt.Errorf("unable to drain: %w", err)
	}
	if stream.Error() != nil {
		return fmt.Errorf("an error occurred during playback: %w", stream.Error())
	}
	if stream.Underflow() {
		return fmt.Errorf("underflow")
	}
	return nil
}

func (stream *Stream) Close() (err error) {
	defer func() {
		r := recover()
		if r != nil {
			err = fmt.Errorf("got a panic: %v", r)
		}
	}()
	err = stream.Close()
	return
}
