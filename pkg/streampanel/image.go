package streampanel

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"math"
	"net/http"
	"time"

	"github.com/chai2010/webp"
	"github.com/facebookincubator/go-belt/tool/logger"
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

	mimeType := http.DetectContentType(b)

	var img image.Image
	err = nil
	switch mimeType {
	case "image/png":
		img, err = png.Decode(bytes.NewReader(b))
	case "image/webp":
		img, err = webp.Decode(bytes.NewReader(b))
	default:
		return nil, fmt.Errorf("unexpected image type %s", mimeType)
	}
	if err != nil {
		return nil, fmt.Errorf("unable to decode the screenshot: %w", err)
	}

	return img, nil
}

func imgFitTo(src image.Image, size image.Point) image.Image {
	sizeCur := src.Bounds().Max
	factor := math.MaxFloat64
	factor = math.Min(factor, float64(size.X)/float64(sizeCur.X))
	factor = math.Min(factor, float64(size.Y)/float64(sizeCur.Y))
	newWidth := uint(float64(sizeCur.X) * factor)
	newHeight := uint(float64(sizeCur.Y) * factor)
	return resize.Resize(newWidth, newHeight, src, resize.Lanczos3)
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
	logger.Tracef(ctx, "screenshot bounds: %#+v", bounds)
	if bounds.Max.X == 0 || bounds.Max.Y == 0 {
		p.DisplayError(fmt.Errorf("received an empty screenshot"))
		p.screenshoterLocker.Lock()
		if p.screenshoterClose != nil {
			p.screenshoterClose()
		}
		p.screenshoterLocker.Unlock()
		return
	}

	if bounds.Max.X > ScreenshotMaxWidth || bounds.Max.Y > ScreenshotMaxHeight {
		screenshot = imgFitTo(screenshot, bounds.Max)
		logger.Tracef(ctx, "rescaled the screenshot from %#+v to %#+v", bounds, screenshot.Bounds())
	}

	p.setImage(ctx, consts.VarKeyImage(consts.ImageScreenshot), screenshot)
}

func (p *Panel) reinitScreenshoter(ctx context.Context) {
	logger.Debugf(ctx, "reinitScreenshoter")
	defer logger.Debugf(ctx, "/reinitScreenshoter")

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
