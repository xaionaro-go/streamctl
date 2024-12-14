package speech

import (
	"context"
	"io"
	"time"

	"github.com/xaionaro-go/streamctl/pkg/audio"
)

type TranscriptWord struct {
	StartTime  time.Duration
	EndTime    time.Duration
	Text       string
	Confidence float32
	Speaker    string
}

type TranscriptVariant struct {
	Text            string
	TranscriptWords []TranscriptWord
	Confidence      float32
}

type Transcript struct {
	Variants        []TranscriptVariant
	Stability       float32
	AudioChannelNum audio.Channel
	Language        Language
	IsFinal         bool
}

type ToText interface {
	io.Closer
	AudioEncoding() audio.Encoding
	AudioChannels() audio.Channel
	WriteAudio(context.Context, []byte) error
	OutputChan() <-chan *Transcript
}
