package commands

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"math"
	"time"

	"github.com/bamiaux/rez"
	"github.com/chai2010/webp"
	"github.com/facebookincubator/go-belt/tool/logger"
	videodecoder "github.com/xaionaro-go/player/pkg/player/decoder/libav"
	"github.com/xaionaro-go/player/pkg/player/imagerenderer"
	videorenderer "github.com/xaionaro-go/player/pkg/player/imagerenderer"
	"github.com/xaionaro-go/streamctl/pkg/streamd/client"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/consts"
	"github.com/xaionaro-go/xsync"
)

type screenshotSender struct {
	xsync.Mutex
	StreamD                    *client.Client
	VariableKey                consts.VarKey
	stepSize                   time.Duration
	PlayerPriority             *xsync.Map[*videodecoder.Decoder, int]
	LastPTS                    map[*videodecoder.Decoder]time.Duration
	ImageResized               map[*videodecoder.Decoder]image.Image
	Buffer                     bytes.Buffer
	LastRenderedPlayerPriority int
	LastRenderTS               time.Time
}

var _ videorenderer.ImageRenderer = (*screenshotSender)(nil)

func newScreenshotSender(
	streamD *client.Client,
	variableKey consts.VarKey,
	fps float64,
	playerPriority *xsync.Map[*videodecoder.Decoder, int],
) *screenshotSender {
	return &screenshotSender{
		StreamD:                    streamD,
		VariableKey:                variableKey,
		stepSize:                   time.Duration(float64(time.Second) / fps),
		PlayerPriority:             playerPriority,
		LastPTS:                    map[*videodecoder.Decoder]time.Duration{},
		ImageResized:               map[*videodecoder.Decoder]image.Image{},
		LastRenderedPlayerPriority: math.MaxInt,
	}
}

func (s *screenshotSender) Close() error {
	return nil
}

func (s *screenshotSender) SetImage(ctx context.Context, imgGetter imagerenderer.ImageGetter) error {
	return xsync.DoA2R1(ctx, &s.Mutex, s.setImage, ctx, imgGetter)
}

func (s *screenshotSender) setImage(
	ctx context.Context,
	imgGetter imagerenderer.ImageGetter,
) (_err error) {
	img := imgGetter.(*videodecoder.ImageGeneric)
	if img.Bounds().Empty() {
		return nil
	}
	now := time.Now()

	priority, ok := s.PlayerPriority.Load(img.Decoder)
	if !ok {
		return fmt.Errorf("unknown player")
	}
	if priority > s.LastRenderedPlayerPriority && now.Sub(s.LastRenderTS) <= time.Second {
		return nil
	}
	s.LastRenderedPlayerPriority = priority
	s.LastRenderTS = now

	imageResized := s.ImageResized[img.Decoder]
	if imageResized == nil {
		var err error
		imageResized, err = imgLike(
			img.Image,
			image.Point{X: consts.ScreenshotMaxWidth / 2, Y: consts.ScreenshotMaxHeight / 2},
		)
		if err != nil {
			return fmt.Errorf("unable to create resized image: %w", err)
		}
		logger.Debugf(ctx, "initialized the screenshot sender: image format %T, size %v, resized size %v", img.Image, img.Image.Bounds().Size(), imageResized.Bounds().Size())
		s.ImageResized[img.Decoder] = imageResized
	}

	lastPTS, ok := s.LastPTS[img.Decoder]
	if !ok {
		lastPTS = time.Duration(math.MinInt64)
	}

	pts := img.GetPTSAsDuration()
	if pts < lastPTS+s.stepSize {
		return nil
	}
	s.LastPTS[img.Decoder] = pts

	err := rez.Convert(imageResized, img.Image, rez.NewLanczosFilter(3))
	if err != nil {
		return fmt.Errorf("unable to resize: %w", err)
	}

	s.Buffer.Reset()
	defer s.Buffer.Reset()
	err = webp.Encode(&s.Buffer, imageResized, &webp.Options{
		Lossless: false,
		Quality:  10,
		Exact:    false,
	})
	if err != nil {
		return fmt.Errorf("unable to encode the image with WebP: %w", err)
	}

	err = s.StreamD.SetVariable(ctx, s.VariableKey, s.Buffer.Bytes())
	if err != nil {
		return fmt.Errorf("unable to set the screenshot: %w", err)
	}

	return nil
}

func imgLike(src image.Image, size image.Point) (image.Image, error) {
	sizeCur := src.Bounds().Size()
	factor := math.MaxFloat64
	factor = math.Min(factor, float64(size.X)/float64(sizeCur.X))
	factor = math.Min(factor, float64(size.Y)/float64(sizeCur.Y))
	newWidth := int(float64(sizeCur.X) * factor)
	newHeight := int(float64(sizeCur.Y) * factor)
	newSize := image.Rectangle{Max: image.Point{
		X: newWidth,
		Y: newHeight,
	}}
	var output image.Image

	switch src := src.(type) {
	case *image.RGBA:
		output = image.NewRGBA(newSize)
	case *image.RGBA64:
		output = image.NewRGBA64(newSize)
	case *image.Gray:
		output = image.NewGray(newSize)
	case *image.YCbCr:
		output = image.NewYCbCr(newSize, src.SubsampleRatio)
	default:
		return nil, fmt.Errorf("image format %T is not supported, yet", src)
	}

	return output, nil
}
