package streampanel

import (
	"bytes"
	"context"
	"crypto"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"math"
	"net/http"
	"time"

	"github.com/bamiaux/rez"
	"github.com/chai2010/webp"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/screenshot"
	"github.com/xaionaro-go/streamctl/pkg/screenshoter"
	streamdconsts "github.com/xaionaro-go/streamctl/pkg/streamd/consts"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/consts"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
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
		logger.Error(ctx, fmt.Errorf("unable to set the screenshot: %w", err))
	}
}

func (p *Panel) downloadImage(
	ctx context.Context,
	imageID consts.ImageID,
) ([]byte, bool, error) {
	return xsync.DoA2R3(ctx, &p.imageLocker, p.downloadImageNoLock, ctx, imageID)
}

func (p *Panel) downloadImageNoLock(
	ctx context.Context,
	imageID consts.ImageID,
) ([]byte, bool, error) {
	varKey := consts.VarKeyImage(imageID)

	if oldImage, ok := p.imageLastDownloaded[imageID]; ok {
		hashType := crypto.SHA1
		hasher := hashType.New()
		hasher.Write(oldImage)
		oldHash := hasher.Sum(nil)

		hash, err := p.StreamD.GetVariableHash(ctx, varKey, hashType)
		if err != nil {
			return nil, false, fmt.Errorf("unable to get a screenshot: %w", err)
		}

		logger.Tracef(ctx, "oldHash == %X; newHash == %X", oldHash, hash)
		if bytes.Equal(hash, oldHash) {
			return oldImage, false, nil
		}
	}
	logger.Tracef(ctx, "no image cache, downloading '%s'", varKey)

	b, err := p.StreamD.GetVariable(ctx, varKey)
	if err != nil {
		return nil, false, fmt.Errorf("unable to get a screenshot: %w", err)
	}

	logger.Tracef(ctx, "downloaded %d bytes", len(b))
	bDup := make([]byte, len(b))
	copy(bDup, b)
	p.imageLastDownloaded[imageID] = bDup

	return b, true, nil
}

func (p *Panel) getImage(
	ctx context.Context,
	imageID consts.ImageID,
) (image.Image, bool, error) {
	b, changed, err := p.downloadImage(ctx, imageID)
	if err != nil {
		return nil, false, fmt.Errorf("unable to download image '%s': %w", imageID, err)
	}

	mimeType := http.DetectContentType(b)

	var img image.Image
	err = nil
	switch mimeType {
	case "image/png":
		img, err = png.Decode(bytes.NewReader(b))
	case "image/webp":
		img, err = webp.Decode(bytes.NewReader(b))
	case "image/jpeg":
		img, err = jpeg.Decode(bytes.NewReader(b))
	case "text/plain; charset=utf-8":
		text := string(b)
		if len(text) > 100 {
			text = text[:100] + "..."
		}
		return nil, false, fmt.Errorf("unexpected to get a text (%s): %s", mimeType, text)
	default:
		return nil, false, fmt.Errorf("unexpected image type %s", mimeType)
	}
	if err != nil {
		return nil, false, fmt.Errorf("unable to decode the screenshot: %w", err)
	}

	return img, changed, nil
}

func imgFitTo(src image.Image, size image.Point) (image.Image, error) {
	sizeCur := src.Bounds().Size()
	factor := math.MaxFloat64
	factor = math.Min(factor, float64(size.X)/float64(sizeCur.X))
	factor = math.Min(factor, float64(size.Y)/float64(sizeCur.Y))
	newWidth := int(float64(sizeCur.X) * factor)
	newHeight := int(float64(sizeCur.Y) * factor)
	output := image.NewRGBA(image.Rectangle{Max: image.Point{
		X: newWidth,
		Y: newHeight,
	}})
	err := rez.Convert(output, src, rez.NewBicubicFilter())
	if err != nil {
		return nil, err
	}
	return output, nil
}

func imgFillTo(
	ctx context.Context,
	src image.Image,
	canvasSize image.Point,
	outSize image.Point,
	offset image.Point,
	alignX streamdconsts.AlignX,
	alignY streamdconsts.AlignY,
) image.Image {

	sizeCur := src.Bounds().Size()
	ratioCur := float64(sizeCur.X) / float64(sizeCur.Y)
	ratioNew := float64(canvasSize.X) / float64(canvasSize.Y)

	if ratioCur == ratioNew {
		return src
	}
	if math.IsNaN(ratioNew) {
		return src
	}

	var sizeNew image.Point
	scale := float64(canvasSize.X) / float64(outSize.X)
	if ratioCur < ratioNew {
		sizeNew = image.Point{
			X: int(scale * float64(sizeCur.Y) * ratioNew),
			Y: int(scale * float64(sizeCur.Y)),
		}
	} else {
		sizeNew = image.Point{
			X: int(scale * float64(sizeCur.X)),
			Y: int(scale * float64(sizeCur.X) / ratioNew),
		}
	}

	logger.Tracef(ctx, "ratio: %v -> %v; size: %#+v -> %#+v", ratioCur, ratioNew, sizeCur, sizeNew)
	img := image.NewRGBA(image.Rectangle{
		Max: sizeNew,
	})

	var offsetX, offsetY int
	if ratioCur < ratioNew {
		offsetX = sizeNew.X - sizeCur.X
		switch alignX {
		case streamdconsts.AlignXLeft:
			offsetX *= 0
		case streamdconsts.AlignXMiddle:
			offsetX /= 2
		case streamdconsts.AlignXRight:
			offsetX /= 1
		}
	} else {
		offsetY = sizeNew.Y - sizeCur.Y
		switch alignY {
		case streamdconsts.AlignYTop:
			offsetY *= 0
		case streamdconsts.AlignYMiddle:
			offsetY /= 2
		case streamdconsts.AlignYBottom:
			offsetY /= 1
		}
	}
	offsetX += offset.X
	offsetY += offset.Y

	for x := 0; x < sizeCur.X; x++ {
		for y := 0; y < sizeCur.Y; y++ {
			xNew := x + offsetX
			yNew := y + offsetY

			img.Set(xNew, yNew, src.At(x, y))
		}
	}

	return img
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
		p.screenshoterLocker.Do(ctx, func() {
			if p.screenshoterClose != nil {
				p.screenshoterClose()
			}
		})
		return
	}

	if bounds.Max.X > ScreenshotMaxWidth || bounds.Max.Y > ScreenshotMaxHeight {
		var err error
		screenshot, err = imgFitTo(screenshot, image.Point{
			X: ScreenshotMaxWidth,
			Y: ScreenshotMaxHeight,
		})
		if err != nil {
			logger.Errorf(ctx, "unable to rescale the screenshot: %w", err)
			return
		}
		logger.Tracef(ctx, "rescaled the screenshot from %#+v to %#+v", bounds, screenshot.Bounds())
	}

	p.setImage(ctx, consts.VarKeyImage(consts.ImageScreenshot), screenshot)
}

func (p *Panel) reinitScreenshoter(ctx context.Context) {
	logger.Debugf(ctx, "reinitScreenshoter")
	defer logger.Debugf(ctx, "/reinitScreenshoter")

	p.screenshoterLocker.Do(ctx, func() {
		if p.screenshoterClose != nil {
			p.screenshoterClose()
			p.screenshoterClose = nil
		}

		if p.Config.Screenshot.Enabled == nil || !*p.Config.Screenshot.Enabled {
			return
		}

		ctx, cancelFunc := context.WithCancel(ctx)
		p.screenshoterClose = cancelFunc
		observability.Go(ctx, func() {
			p.Screenshoter.Loop(
				ctx,
				200*time.Millisecond,
				p.Config.Screenshot.Config,
				func(ctx context.Context, img *image.RGBA) { p.setScreenshot(ctx, img) },
			)
		})
	})
}
