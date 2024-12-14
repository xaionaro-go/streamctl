package speechtotext

import (
	"context"
	"fmt"
	"sync"

	googleapi "cloud.google.com/go/speech/apiv2"
	"cloud.google.com/go/speech/apiv2/speechpb"
	"github.com/xaionaro-go/streamctl/pkg/audio"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/speech"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

type SpeechToText struct {
	wg         sync.WaitGroup
	googleAPI  *googleapi.Client
	cancelFunc context.CancelFunc
	opQueue    chan *googleapi.BatchRecognizeOperation
}

var _ speech.ToText = (*SpeechToText)(nil)

func New() (*SpeechToText, error) {
	ctx := context.Background()
	ctx, cancelFunc := context.WithCancel(ctx)
	googleAPI, err := googleapi.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize a client to Google Speech API: %w", err)
	}

	stt := &SpeechToText{
		googleAPI:  googleAPI,
		opQueue:    make(chan *googleapi.BatchRecognizeOperation, 1024),
		cancelFunc: cancelFunc,
	}

	stt.wg.Add(1)
	observability.Go(ctx, func() {
		defer stt.wg.Done()
		stt.loop(ctx)
	})

	return stt, nil
}

func (stt *SpeechToText) loop(ctx context.Context) {
	for {
		select {
		case op := <-stt.opQueue:
			resp, err := op.Wait(ctx)

		}
	}
}

func (stt *SpeechToText) AudioEncoding() audio.Encoding {
	return stt.audioEncoding
}

func (stt *SpeechToText) WriteAudio(
	ctx context.Context,
	audio []byte,
) error {

	req := &speechpb.BatchRecognizeRequest{
		Recognizer:              "",
		Config:                  &speechpb.RecognitionConfig{},
		ConfigMask:              &fieldmaskpb.FieldMask{},
		Files:                   []*speechpb.BatchRecognizeFileMetadata{},
		RecognitionOutputConfig: &speechpb.RecognitionOutputConfig{},
		ProcessingStrategy:      0,
	}
	op, err := stt.googleAPI.BatchRecognize(ctx, req)
	if err != nil {
		// TODO: Handle error.
	}

}

func (stt *SpeechToText) OutputChan() <-chan string {

}

func (stt *SpeechToText) Close() error {
	if stt.cancelFunc == nil {
		return fmt.Errorf("already closed!")
	}
	stt.cancelFunc()
	stt.cancelFunc = nil
	err := stt.googleAPI.Close()
	stt.waitForClosure()
	close(stt.opQueue)
	return err
}

func (stt *SpeechToText) waitForClosure() {
	stt.wg.Wait()
}
