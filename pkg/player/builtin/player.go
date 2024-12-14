package builtin

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"sync/atomic"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/audio"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/recoder/libav/recoder"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
)

const (
	BufferSizeAudio = 100 * time.Millisecond
)

type Player struct {
	ImageRenderer
	AudioRenderer
	lastSeekAt           time.Time
	audioWriter          io.WriteCloser
	audioStream          audio.Stream
	locker               xsync.Gorex
	currentURL           string
	currentImage         image.Image
	currentDuration      time.Duration
	currentVideoPosition time.Duration
	currentAudioPosition time.Duration
	videoStreamIndex     atomic.Uint32
	audioStreamIndex     atomic.Uint32
	endChan              chan struct{}
}

func New(
	ctx context.Context,
	imageRenderer ImageRenderer,
	audioRenderer AudioRenderer,
) *Player {
	p := &Player{
		ImageRenderer: imageRenderer,
		AudioRenderer: audioRenderer,
		endChan:       make(chan struct{}),
	}
	p.onEnd()
	return p
}

func (*Player) SetupForStreaming(
	ctx context.Context,
) error {
	panic("not implemented, yet")
}

func (p *Player) OpenURL(
	ctx context.Context,
	link string,
) (_err error) {
	logger.Debugf(ctx, "OpenURL(ctx, '%s')", link)
	defer func() { logger.Debugf(ctx, "/OpenURL(ctx, '%s'): %v", link, _err) }()
	return xsync.DoA2R1(ctx, &p.locker, p.openURL, ctx, link)
}

func (p *Player) openURL(
	ctx context.Context,
	link string,
) error {
	decoderCfg := recoder.DecoderConfig{}
	decoder, err := recoder.NewDecoder(decoderCfg)
	logger.Tracef(ctx, "NewDecoder(%#+v): %v", decoderCfg, err)
	if err != nil {
		return fmt.Errorf("unable to initialize a decoder: %w", err)
	}

	inputCfg := recoder.InputConfig{}
	input, err := recoder.NewInputFromURL(ctx, link, "", inputCfg)
	logger.Tracef(ctx, "NewInputFromURL(ctx, '%s', '', %#+v): %v", link, inputCfg, err)
	if err != nil {
		return fmt.Errorf("unable to open '%s': %w", link, err)
	}

	fr := p.newFrameReader(ctx)
	err = decoder.ReadFrame(ctx, input, fr)
	logger.Tracef(ctx, "ReadFrame(ctx, input, frameReader): %v", err)
	if err != nil {
		return fmt.Errorf("unable to start reading the streams from '%s': %w", link, err)
	}
	p.onSeek(ctx)
	observability.Go(ctx, func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			err := decoder.ReadFrame(ctx, input, fr)
			switch {
			case err == nil:
				continue
			case errors.Is(err, io.EOF):
			default:
				logger.Errorf(ctx, "got an error while reading the streams from '%s': %v", link, err)
			}
			p.onEnd()
			return
		}
	})

	p.currentURL = link
	if p.ImageRenderer != nil {
		if err := p.ImageRenderer.SetVisible(true); err != nil {
			return fmt.Errorf("unable to make the image renderer visible: %w", err)
		}
	}
	return nil
}

func (p *Player) processFrame(
	ctx context.Context,
	frame *recoder.Frame,
) error {
	logger.Tracef(ctx, "processFrame: pos: %v; dur: %v; pts: %v; time_base: %v", frame.Position(), frame.MaxPosition(), frame.Pts(), frame.DecoderContext.TimeBase())
	defer func() {
		logger.Tracef(ctx, "/processFrame; av-desync: %v", p.currentAudioPosition-p.currentVideoPosition)
	}()
	return xsync.DoR1(ctx, &p.locker, func() error {
		switch frame.DecoderContext.MediaType() {
		case MediaTypeVideo:
			return p.processVideoFrame(ctx, frame)
		case MediaTypeAudio:
			return p.processAudioFrame(ctx, frame)
		default:
			// we don't care about everything else
			return nil
		}
	})
}

func (p *Player) onSeek(
	ctx context.Context,
) {
	logger.Tracef(ctx, "onSeek")
	defer logger.Tracef(ctx, "/onSeek")

	p.lastSeekAt = time.Now()
}

func (p *Player) processVideoFrame(
	ctx context.Context,
	frame *recoder.Frame,
) error {
	logger.Tracef(ctx, "processVideoFrame")
	defer logger.Tracef(ctx, "/processVideoFrame")
	if p.ImageRenderer == nil {
		return nil
	}

	p.currentVideoPosition = frame.Position()
	p.currentDuration = frame.MaxPosition()

	streamIdx := frame.Packet.StreamIndex()

	if p.videoStreamIndex.CompareAndSwap(math.MaxUint32, uint32(streamIdx)) { // atomics are not really needed because all of this happens while holding p.locker
		if err := p.initImageFor(ctx, frame); err != nil {
			return fmt.Errorf("unable to initialize an image variable for the frame: %w", err)
		}
	} else {
		oldStreamIdx := int(p.videoStreamIndex.Load())
		if oldStreamIdx != streamIdx {
			return fmt.Errorf("the index of the video stream have changed from %d to %d; the support of dynamic/multiple video tracks is not implemented, yet", oldStreamIdx, streamIdx)
		}
	}

	frame.Data().ToImage(p.currentImage)

	if err := p.renderCurrentPicture(); err != nil {
		return fmt.Errorf("unable to render the picture: %w", err)
	}

	sinceStart := time.Since(p.lastSeekAt)
	nextExpectedPosition := p.currentVideoPosition + frame.FrameDuration()
	waitIntervalForNextFrame := nextExpectedPosition - sinceStart
	logger.Tracef(ctx, "sleeping for %v (%v - %v)", waitIntervalForNextFrame, nextExpectedPosition, sinceStart)
	time.Sleep(waitIntervalForNextFrame)

	return nil
}

func (p *Player) renderCurrentPicture() error {
	return p.ImageRenderer.Render()
}

func (p *Player) processAudioFrame(
	ctx context.Context,
	frame *recoder.Frame,
) error {
	logger.Tracef(ctx, "processAudioFrame")
	defer logger.Tracef(ctx, "/processAudioFrame")
	if p.AudioRenderer == nil {
		return nil
	}

	p.currentAudioPosition = frame.Position()
	streamIdx := frame.Packet.StreamIndex()

	if p.audioStreamIndex.CompareAndSwap(math.MaxUint32, uint32(streamIdx)) { // atomics are not really needed because all of this happens while holding p.locker
		r, w := io.Pipe()
		p.audioWriter = w
		sampleRate := frame.DecoderContext.SampleRate()
		channels := frame.DecoderContext.ChannelLayout().Channels()
		pcmFormat := frame.DecoderContext.SampleFormat()
		codecID := frame.DecoderContext.CodecID()
		logger.Debugf(ctx, "codecID == %v, sampleRate == %v, channels == %v, pcmFormat == %v", codecID, sampleRate, channels, pcmFormat)
		bufferSize := BufferSizeAudio
		audioStream, err := p.AudioRenderer.PlayPCM(
			audio.SampleRate(sampleRate),
			audio.Channel(channels),
			pcmFormatToAudio(pcmFormat),
			bufferSize,
			r,
		)
		if err != nil {
			return fmt.Errorf("unable to initialize an audio playback: %w", err)
		}
		p.audioStream = audioStream
	} else {
		oldStreamIdx := int(p.audioStreamIndex.Load())
		if oldStreamIdx != streamIdx {
			logger.Tracef(ctx, "we do not support multiple audio streams, yet; so we ignore this new stream, index: %d (which is not %d)", streamIdx, oldStreamIdx)
			return nil
		}
	}

	align := 1
	frameBytes, err := frame.Data().Bytes(int(align))
	if err != nil {
		return fmt.Errorf("unable to get the audio frame data: %w", err)
	}

	n, err := p.audioWriter.Write(frameBytes)
	if err != nil {
		return fmt.Errorf("unable to write the audio frame into the playback subsystem: %w", err)
	}
	if n != len(frameBytes) {
		return fmt.Errorf("unable to write the full audio frame: %d != %d", n, len(frameBytes))
	}

	return nil
}

func (p *Player) onEnd() {
	ctx := context.TODO()
	logger.Debugf(ctx, "onEnd")
	defer logger.Debugf(ctx, "/onEnd")
	p.locker.Do(ctx, func() {
		p.videoStreamIndex.Store(math.MaxUint32)
		p.audioStreamIndex.Store(math.MaxUint32)
		p.currentURL = ""
		if p.audioWriter != nil {
			p.audioWriter.Close()
			p.audioWriter = nil
		}
		if p.audioStream != nil {
			p.audioStream.Close()
			p.audioStream = nil
		}

		var oldEndChan chan struct{}
		p.endChan, oldEndChan = make(chan struct{}), p.endChan
		close(oldEndChan)
		if p.ImageRenderer != nil {
			if err := p.ImageRenderer.SetVisible(false); err != nil {
				logger.Errorf(ctx, "unable to close ImageRenderer: %v", err)
			}
		}
	})
}

func (p *Player) EndChan(
	ctx context.Context,
) (<-chan struct{}, error) {
	return p.endChan, nil
}

func (p *Player) IsEnded(
	ctx context.Context,
) (bool, error) {
	return xsync.DoR1(ctx, &p.locker, p.isEnded), nil
}

func (p *Player) isEnded() bool {
	return p.currentURL != ""
}

func (p *Player) GetPosition(
	ctx context.Context,
) (time.Duration, error) {
	return xsync.DoR2(ctx, &p.locker, func() (time.Duration, error) {
		if p.isEnded() {
			return 0, fmt.Errorf("the player is not started or already ended")
		}

		return (p.currentVideoPosition + p.currentAudioPosition) / 2, nil
	})
}

func (p *Player) GetLength(
	ctx context.Context,
) (time.Duration, error) {
	return xsync.DoR2(ctx, &p.locker, func() (time.Duration, error) {
		if p.isEnded() {
			return 0, fmt.Errorf("the player is not started or already ended")
		}

		return p.currentDuration, nil
	})
}

func (p *Player) ProcessTitle(
	ctx context.Context,
) (string, error) {
	if titler, ok := p.ImageRenderer.(interface{ Title() string }); ok {
		return titler.Title(), nil
	}
	return "", nil
}

func (p *Player) GetLink(
	ctx context.Context,
) (string, error) {
	return xsync.DoR2(ctx, &p.locker, func() (string, error) {
		if p.isEnded() {
			return "", fmt.Errorf("the player is not started or already ended")
		}

		return p.currentURL, nil
	})
}

func (*Player) GetSpeed(
	ctx context.Context,
) (float64, error) {
	logger.Errorf(ctx, "GetSpeed is not implemented, yet")
	return 1, nil
}

func (*Player) SetSpeed(
	ctx context.Context,
	speed float64,
) error {
	logger.Errorf(ctx, "SetSpeed is not implemented, yet")
	return nil
}

func (*Player) GetPause(
	ctx context.Context,
) (bool, error) {
	panic("not implemented, yet")
}

func (*Player) SetPause(
	ctx context.Context,
	pause bool,
) error {
	logger.Errorf(ctx, "SetPause is not implemented, yet")
	return nil
}

func (*Player) Stop(
	ctx context.Context,
) error {
	panic("not implemented, yet")
}

func (*Player) Close(ctx context.Context) error {
	return fmt.Errorf("not implemented, yet")
}
