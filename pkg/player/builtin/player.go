package builtin

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/recoder/libav/recoder"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
	"github.com/xaionaro-go/typing/ordered"
)

type Player struct {
	window               fyne.Window
	locker               xsync.Mutex
	currentURL           string
	currentImage         image.Image
	currentDuration      time.Duration
	currentVideoPosition time.Duration
	currentAudioPosition time.Duration
	videoStreamIndex     ordered.Optional[int]
	audioStreamIndex     ordered.Optional[int]
	canvasImage          *canvas.Image
	endChan              chan struct{}
}

func New(
	ctx context.Context,
	title string,
) *Player {
	w := fyne.CurrentApp().NewWindow(title)
	return &Player{
		window: w,
	}
}

func (*Player) SetupForStreaming(
	ctx context.Context,
) error {
	panic("not implemented, yet")
}

func (p *Player) OpenURL(
	ctx context.Context,
	link string,
) error {
	return xsync.DoA2R1(ctx, &p.locker, p.openURL, ctx, link)
}

func (p *Player) openURL(
	ctx context.Context,
	link string,
) error {
	decoder, err := recoder.NewDecoder(recoder.DecoderConfig{})
	if err != nil {
		return fmt.Errorf("unable to initialize a decoder: %w", err)
	}

	input, err := recoder.NewInputFromURL(ctx, link, "", recoder.InputConfig{})
	if err != nil {
		return fmt.Errorf("unable to open '%s': %w", link, err)
	}

	fr := p.newFrameReader(ctx)
	err = decoder.ReadFrame(ctx, input, fr)
	if err != nil {
		return fmt.Errorf("unable to start reading the streams from '%s': %w", link, err)
	}
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
				logger.Errorf(ctx, "got an error while reading the streams from '%s': %w", link, err)
			}
			p.onEnd()
			return
		}
	})

	p.currentURL = link
	p.window.Show()
	return nil
}

func (p *Player) processFrame(
	ctx context.Context,
	frame *recoder.Frame,
) error {
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

func (p *Player) processVideoFrame(
	ctx context.Context,
	frame *recoder.Frame,
) error {
	p.currentVideoPosition = frame.Position()
	p.currentDuration = frame.Duration()
	logger.Tracef(ctx, "pos: %v; dur: %v", p.currentVideoPosition, p.currentDuration)

	streamIdx := frame.Packet.StreamIndex()
	if p.videoStreamIndex.IsSet() {
		if p.videoStreamIndex.Get() != streamIdx {
			return fmt.Errorf("the index of the video stream have changed from %d to %d; the support of dynamic/multiple video tracks is not implemented, yet", p.videoStreamIndex.Get(), streamIdx)
		}
	} else {
		if err := p.initImageFor(ctx, frame); err != nil {
			return fmt.Errorf("unable to initialize an image variable for the frame: %w", err)
		}
		p.videoStreamIndex.Set(streamIdx)
	}

	frame.Data().ToImage(p.currentImage)

	if err := p.renderCurrentPicture(); err != nil {
		return fmt.Errorf("unable to render the picture: %w", err)
	}

	// DELETE ME: {
	logger.Errorf(ctx, "DELETE ME")
	time.Sleep(time.Millisecond * 1000 / 30)
	// }

	return nil
}

func (p *Player) renderCurrentPicture() error {
	p.canvasImage.Refresh()
	return nil
}

func (p *Player) processAudioFrame(
	ctx context.Context,
	frame *recoder.Frame,
) error {
	p.currentAudioPosition = frame.Position()
	streamIdx := frame.Packet.StreamIndex()

	if p.audioStreamIndex.IsSet() && p.audioStreamIndex.Get() != streamIdx {
		return fmt.Errorf("the index of the audio stream have changed from %d to %d; the support of dynamic/multiple audio tracks is not implemented, yet", p.audioStreamIndex.Get(), streamIdx)
	}

	logger.Errorf(ctx, "the support of audio is not implemented")

	return nil
}

func (p *Player) onEnd() {
	ctx := context.TODO()
	p.locker.Do(ctx, func() {
		p.currentURL = ""

		var oldEndChan chan struct{}
		p.endChan, oldEndChan = make(chan struct{}), p.endChan
		close(oldEndChan)
		p.window.Hide()
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
	return p.window.Title(), nil
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
	panic("not implemented, yet")
}
