package whisper

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/streamctl/pkg/audio"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/speech"
)

const (
	ModelName = ""
)

type SpeechToText struct {
	closeCount    atomic.Uint64
	wg            sync.WaitGroup
	whisperClient io.ReadWriteCloser
	cancelFunc    context.CancelFunc
	resultQueue   chan *speech.Transcript
}

var _ speech.ToText = (*SpeechToText)(nil)

func New(
	ctx context.Context,
	whisperClient io.ReadWriteCloser,
	shouldClose bool,
) *SpeechToText {
	ctx, cancelFunc := context.WithCancel(ctx)

	if !shouldClose {
		whisperClient = noopCloser{whisperClient}
	}

	stt := &SpeechToText{
		whisperClient: whisperClient,
		resultQueue:   make(chan *speech.Transcript, 1024),
		cancelFunc:    cancelFunc,
	}

	stt.wg.Add(1)
	observability.Go(ctx, func() {
		defer stt.wg.Done()
		defer stt.Close()
		err := stt.loop(ctx)
		if err != nil {
			select {
			case <-ctx.Done():
			default:
				logger.Errorf(ctx, "stt.loop returned error: %v", err)
			}
		}
	})

	return stt
}

func (stt *SpeechToText) AudioEncoding() audio.Encoding {
	return audio.EncodingPCM{
		PCMFormat:  audio.PCMFormatS16LE,
		SampleRate: 16000,
	}
}

func (stt *SpeechToText) AudioChannels() audio.Channel {
	return 1
}

func (stt *SpeechToText) loop(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "stt.loop()")
	defer func() { logger.Debugf(ctx, "/stt.loop(): %v", _err) }()

	buf := make([]byte, 1024*1024)
	for {
		n, err := stt.whisperClient.Read(buf)
		if err != nil {
			return fmt.Errorf("unable to read from the whisper server: %w", err)
		}
		if n == len(buf) {
			return fmt.Errorf("received too big message")
		}
		if n == 0 {
			return fmt.Errorf("received zero bytes")
		}

		msg := string(buf[:n])
		text, words, err := parseMessage(ctx, msg)
		if err != nil {
			return fmt.Errorf("unable to parse whisper output '%s' (%X): %w", msg, msg, err)
		}
		stt.resultQueue <- &speech.Transcript{
			Variants: []speech.TranscriptVariant{{
				Text:            text,
				TranscriptWords: words,
				Confidence:      0.5,
			}},
			Stability:       0.5,
			AudioChannelNum: 0,
			Language:        "",
			IsFinal:         true,
		}
	}
}

func (stt *SpeechToText) WriteAudio(
	ctx context.Context,
	audio []byte,
) error {
	n, err := stt.whisperClient.Write(audio)
	if err != nil {
		return fmt.Errorf("unable to write audio: %w", err)
	}
	if n != len(audio) {
		return fmt.Errorf("written message is too short: %d < %d", n, len(audio))
	}
	return nil
}

func (stt *SpeechToText) OutputChan() <-chan *speech.Transcript {
	return stt.resultQueue
}

func (stt *SpeechToText) Close() error {
	if stt.closeCount.Add(1) != 1 {
		return fmt.Errorf("already closed")
	}

	stt.cancelFunc()
	stt.cancelFunc = nil

	var mErr *multierror.Error

	mErr = multierror.Append(mErr, stt.whisperClient.Close())
	stt.waitForClosure()
	close(stt.resultQueue)
	return mErr.ErrorOrNil()
}

func (stt *SpeechToText) waitForClosure() {
	stt.wg.Wait()
}
