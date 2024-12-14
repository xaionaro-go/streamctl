package subtitleswindow

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/audio"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/speech/speechtotext/whisper"
)

type speechStream struct {
	cancelFunc       context.CancelFunc
	audioReader      io.Reader
	whisperClient    *whisper.SpeechToText
	recognizerCloser io.Closer
	wg               sync.WaitGroup
	onceCloser       onceCloser
}

var _ audio.Stream = (*speechStream)(nil)

func newSpeechStream(
	ctx context.Context,
	audioReader io.Reader,
	whisperClient *whisper.SpeechToText,
	recognizerCloser io.Closer,
) *speechStream {
	ctx, cancelFunc := context.WithCancel(ctx)
	s := &speechStream{
		cancelFunc:       cancelFunc,
		audioReader:      audioReader,
		whisperClient:    whisperClient,
		recognizerCloser: recognizerCloser,
	}
	s.init(ctx)
	return s
}

func (s *speechStream) init(ctx context.Context) {
	s.wg.Add(1)
	observability.Go(ctx, func() {
		defer s.wg.Done()
		defer s.Close()
		err := s.loop(ctx)
		if err != nil {
			select {
			case <-ctx.Done():
			default:
				logger.Errorf(ctx, "loop returned error: %v", err)
			}
		}
	})
}
func (s *speechStream) loop(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "loop()")
	defer func() { logger.Debugf(ctx, "/loop(): %v", _err) }()

	buf := make([]byte, 1024*1024)
	for {
		logger.Tracef(ctx, "Read()")
		n, err := s.audioReader.Read(buf)
		logger.Tracef(ctx, "/Read(): %v %v", n, err)
		if err != nil {
			return fmt.Errorf("unable to read audio from the reader: %w", err)
		}
		if n == len(buf) {
			return fmt.Errorf("message is too long; not implemented yet")
		}
		msg := buf[:n]

		logger.Tracef(ctx, "WriteAudio()")
		err = s.whisperClient.WriteAudio(ctx, msg)
		logger.Tracef(ctx, "/WriteAudio(): %v", err)
		if err != nil {
			return fmt.Errorf("unable to write the audio to the whisper: %w", err)
		}
	}
}

func (s *speechStream) Drain() error {
	return nil
}
func (s *speechStream) Close() error {
	var err error
	s.onceCloser.Do(func() {
		logger.Debugf(context.TODO(), "Close")
		s.cancelFunc()
		err = s.recognizerCloser.Close()
	})
	return err
}
