package streampanel

import (
	"bytes"
	"context"
	"crypto"
	"fmt"
	"image"
	"image/png"
	"math"
	"net/http"
	"time"

	"github.com/chai2010/webp"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/nfnt/resize"
	"github.com/xaionaro-go/streamctl/pkg/observability"
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
		logger.Error(ctx, fmt.Errorf("unable to set the screenshot: %w", err))
	}
}

func (p *Panel) downloadImage(

	ctx context.Context,
	imageID consts.ImageID,
) ([]byte, bool, error) {
	p.imageLocker.Lock()
	defer p.imageLocker.Unlock()
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
	default:
		return nil, false, fmt.Errorf("unexpected image type %s", mimeType)
	}
	if err != nil {
		return nil, false, fmt.Errorf("unable to decode the screenshot: %w", err)
	}

	return img, changed, nil
}

func imgFitTo(src image.Image, size image.Point) image.Image {
	sizeCur := src.Bounds().Size()
	factor := math.MaxFloat64
	factor = math.Min(factor, float64(size.X)/float64(sizeCur.X))
	factor = math.Min(factor, float64(size.Y)/float64(sizeCur.Y))
	newWidth := uint(float64(sizeCur.X) * factor)
	newHeight := uint(float64(sizeCur.Y) * factor)
	return resize.Resize(newWidth, newHeight, src, resize.Lanczos3)
}

type align int

const (
	alignCenter = align(iota)
	alignStart
	alignEnd
)

func imgFillTo(
	ctx context.Context,
	src image.Image,
	size image.Point,
	alignX align,
	alignY align,
) image.Image {
	sizeCur := src.Bounds().Size()
	ratioCur := float64(sizeCur.X) / float64(sizeCur.Y)
	ratioNew := float64(size.X) / float64(size.Y)

	if ratioCur == ratioNew {
		return src
	}
	if math.IsNaN(ratioNew) {
		return src
	}

	var sizeNew image.Point
	if ratioCur < ratioNew {
		sizeNew = image.Point{
			X: int(float64(sizeCur.Y) * ratioNew),
			Y: sizeCur.Y,
		}
	} else {
		sizeNew = image.Point{
			X: sizeCur.X,
			Y: int(float64(sizeCur.X) / ratioNew),
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
		case alignStart:
			offsetX *= 0
		case alignCenter:
			offsetX /= 2
		case alignEnd:
			offsetX /= 1
		}
	} else {
		offsetY = sizeNew.Y - sizeCur.Y
		switch alignY {
		case alignStart:
			offsetY *= 0
		case alignCenter:
			offsetY /= 2
		case alignEnd:
			offsetY /= 1
		}
	}

	for x := 0; x < sizeCur.X; x++ {
		for y := 0; y < sizeCur.Y; y++ {
			xNew := x + offsetX
			yNew := y + offsetY

			img.Set(xNew, yNew, src.At(x, y))
		}
	}

	return img
}

func imgRotateFillTo(
	ctx context.Context,
	src image.Image,
	size image.Point,
	alignX align,
	alignY align,
) image.Image {
	return imgFillTo(
		ctx,
		src,
		size,
		alignX, alignY,
	)
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
		screenshot = imgFitTo(screenshot, image.Point{
			X: ScreenshotMaxWidth,
			Y: ScreenshotMaxHeight,
		})
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
	observability.Go(ctx, func() {
		p.Screenshoter.Loop(
			ctx,
			200*time.Millisecond,
			p.Config.Screenshot.Config,
			func(ctx context.Context, img *image.RGBA) { p.setScreenshot(ctx, img) },
		)
	})
}
