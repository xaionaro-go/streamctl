package streampanel

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"math"
	"time"

	"github.com/chai2010/webp"
	"github.com/nfnt/resize"
	"github.com/xaionaro-go/streamctl/pkg/screenshot"
	"github.com/xaionaro-go/streamctl/pkg/screenshoter"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/consts"
)

type Screenshoter interface {
	Engine() screenshoter.ScreenshotEngine
	Loop(
		ctx context.Context,
		interval time.Duration,
		config screenshot.Config,
		callback func(context.Context, *image.RGBA),
	)
}

func (p *Panel) setImage(
	ctx context.Context,
	key consts.VarKey,
	screenshot image.Image,
) {
	var buf bytes.Buffer
	err := webp.Encode(&buf, screenshot, &webp.Options{
		Lossless: false,
		Quality:  10,
		Exact:    false,
	})
	if err != nil {
		p.DisplayError(fmt.Errorf("unable to encode the screenshot with WebP: %w", err))
		return
	}
	b := buf.Bytes()

	err = p.StreamD.SetVariable(ctx, key, b)
	if err != nil {
		p.DisplayError(fmt.Errorf("unable to set the screenshot: %w", err))
	}
}

func (p *Panel) getImage(
	ctx context.Context,
	key consts.VarKey,
) (image.Image, error) {
	b, err := p.StreamD.GetVariable(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("unable to get a screenshot: %w", err)
	}

	img, err := webp.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("unable to decode the screenshot: %w", err)
	}

	return img, nil
}

const (
	ScreenshotMaxWidth  = 384
	ScreenshotMaxHeight = 216
)

func (p *Panel) setScreenshot(
	ctx context.Context,
	screenshot image.Image,
) {
	bounds := screenshot.Bounds()
	if bounds.Max.X > ScreenshotMaxWidth || bounds.Max.Y > ScreenshotMaxHeight {
		factor := 1.0
		factor = math.Min(factor, float64(ScreenshotMaxWidth)/float64(bounds.Max.X))
		factor = math.Min(factor, float64(ScreenshotMaxHeight)/float64(bounds.Max.Y))
		newWidth := uint(float64(bounds.Max.X) * factor)
		newHeight := uint(float64(bounds.Max.Y) * factor)
		screenshot = resize.Resize(newWidth, newHeight, screenshot, resize.Lanczos3)
	}

	p.setImage(ctx, consts.VarKeyImage(consts.ImageScreenshot), screenshot)
}

func (p *Panel) reinitScreenshoter(ctx context.Context) {
	p.screenshoterLocker.Lock()
	defer p.screenshoterLocker.Unlock()
	if p.screenshoterClose != nil {
		p.screenshoterClose()
		p.screenshoterClose = nil
	}

	if p.Config.Screenshot.Enabled == nil || !*p.Config.Screenshot.Enabled {
		return
	}

	ctx, cancelFunc := context.WithCancel(ctx)
	p.screenshoterClose = cancelFunc
	go p.Screenshoter.Loop(
		ctx,
		time.Second,
		p.Config.Screenshot.Config,
		func(ctx context.Context, img *image.RGBA) { p.setScreenshot(ctx, img) },
	)
}
