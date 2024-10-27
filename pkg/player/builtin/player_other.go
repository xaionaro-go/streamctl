//go:build !android
// +build !android

package builtin

import (
	"context"
	"fmt"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/recoder/libav/recoder"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
)

func (p *Player) processFrame(
	ctx context.Context,
	frame *recoder.Frame,
) error {
	return xsync.DoR1(ctx, &p.locker, func() error {
		//stream.CodecParameters().MediaType()
		switch frame.DecoderContext.MediaType() {
		case astiav.MediaTypeVideo:
			return p.processVideoFrame(ctx, frame)
		case astiav.MediaTypeAudio:
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
		p.currentDuration = frame.Duration()
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
