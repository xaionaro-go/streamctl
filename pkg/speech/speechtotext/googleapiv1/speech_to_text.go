package googleapiv1

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	googleapi "cloud.google.com/go/speech/apiv1"
	"cloud.google.com/go/speech/apiv1/speechpb"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/streamctl/pkg/audio"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/speech"
)

type SpeechToText struct {
	closeCount    atomic.Uint64
	wg            sync.WaitGroup
	audioEncoding audio.Encoding
	audioChannels audio.Channel
	googleAPI     *googleapi.Client
	stream        speechpb.Speech_StreamingRecognizeClient
	cancelFunc    context.CancelFunc
	resultQueue   chan *speech.Transcript
}

var _ speech.ToText = (*SpeechToText)(nil)

func New(
	ctx context.Context,
	language speech.Language,
	audioEncoding audio.Encoding,
	audioChannels audio.Channel,
) (*SpeechToText, error) {
	thriftAudioEncoding, thriftSampleRate, err := AudioEncodingToThrift(audioEncoding)
	if err != nil {
		return nil, fmt.Errorf("unable to convert the audio encoding for Google API: %w", err)
	}

	ctx, cancelFunc := context.WithCancel(ctx)

	googleAPI, err := googleapi.NewClient(ctx)
	if err != nil {
		cancelFunc()
		return nil, fmt.Errorf("unable to initialize a client to Google Speech API: %w", err)
	}

	stream, err := googleAPI.StreamingRecognize(ctx)
	if err != nil {
		cancelFunc()
		return nil, fmt.Errorf("unable to initialize a recognition stream: %w", err)
	}

	err = stream.Send(&speechpb.StreamingRecognizeRequest{
		StreamingRequest: &speechpb.StreamingRecognizeRequest_StreamingConfig{
			StreamingConfig: &speechpb.StreamingRecognitionConfig{
				Config: &speechpb.RecognitionConfig{
					Encoding:          thriftAudioEncoding,
					SampleRateHertz:   thriftSampleRate,
					LanguageCode:      string(language),
					AudioChannelCount: int32(audioChannels),
				},
			},
		},
	})
	if err != nil {
		cancelFunc()
		return nil, fmt.Errorf("unable to send the configuration to the stream: %w", err)
	}

	stt := &SpeechToText{
		audioEncoding: audioEncoding,
		audioChannels: audioChannels,
		googleAPI:     googleAPI,
		stream:        stream,
		resultQueue:   make(chan *speech.Transcript, 1024),
		cancelFunc:    cancelFunc,
	}

	stt.wg.Add(1)
	observability.Go(ctx, func() {
		defer stt.wg.Done()
		defer stt.Close()
		err := stt.loop(ctx)
		if err != nil {
			logger.Errorf(ctx, "stt.loop returned error: %v", err)
		}
	})

	return stt, nil
}

func (stt *SpeechToText) AudioEncoding() audio.Encoding {
	return stt.audioEncoding
}

func (stt *SpeechToText) AudioChannels() audio.Channel {
	return stt.audioChannels
}

func (stt *SpeechToText) loop(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "stt.loop()")
	defer func() { logger.Debugf(ctx, "/stt.loop(): %v", _err) }()

	for {
		resp, err := stt.stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("unable to read a result from the stream: %w", err)
		}
		if resp == nil {
			return fmt.Errorf("resp == nil")
		}
		if resp.Error != nil {
			return fmt.Errorf("resp.Error != nil: %d: %s", resp.Error.GetCode(), resp.Error.GetMessage())
		}

		for _, result := range resp.GetResults() {
			transcript := speech.Transcript{
				IsFinal: result.GetIsFinal(),
			}
			for _, alt := range result.GetAlternatives() {
				logger.Debugf(ctx, "confidence: %.3f, transcript: <%s>", alt.GetTranscript())
				variant := speech.TranscriptVariant{
					Confidence: alt.GetConfidence(),
				}
				for _, word := range alt.Words {
					variant.TranscriptWords = append(
						variant.TranscriptWords,
						speech.TranscriptWord{
							StartTime:  word.GetStartTime().AsDuration(),
							EndTime:    word.GetEndTime().AsDuration(),
							Text:       word.GetWord(),
							Confidence: word.GetConfidence(),
							Speaker:    word.GetSpeakerLabel(),
						},
					)
				}
				transcript.Variants = append(transcript.Variants, variant)
			}
			stt.resultQueue <- &transcript
		}
	}
}

func (stt *SpeechToText) WriteAudio(
	ctx context.Context,
	audio []byte,
) error {
	err := stt.stream.Send(&speechpb.StreamingRecognizeRequest{
		StreamingRequest: &speechpb.StreamingRecognizeRequest_AudioContent{
			AudioContent: audio,
		},
	})
	if err != nil {
		return fmt.Errorf("unable to write audio: %w", err)
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

	mErr = multierror.Append(mErr, stt.stream.CloseSend())
	mErr = multierror.Append(mErr, stt.googleAPI.Close())
	stt.waitForClosure()
	close(stt.resultQueue)
	return mErr.ErrorOrNil()
}

func (stt *SpeechToText) waitForClosure() {
	stt.wg.Wait()
}
