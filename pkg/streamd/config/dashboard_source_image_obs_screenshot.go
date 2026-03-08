package config

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"time"

	"github.com/chai2010/webp"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
	"github.com/xaionaro-go/streamctl/pkg/clock"
	"github.com/xaionaro-go/streamctl/pkg/imgb64"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

type DashboardSourceImageOBSScreenshot struct {
	Name           string      `yaml:"name"            json:"name"`
	Width          float64     `yaml:"width"           json:"width"`
	Height         float64     `yaml:"height"          json:"height"`
	ImageFormat    ImageFormat `yaml:"image_format"    json:"image_format"`
	UpdateInterval Duration    `yaml:"update_interval" json:"update_interval"`
}

var _ SourceImage = (*DashboardSourceImageOBSScreenshot)(nil)
var _ GetImageFromOBSer = (*DashboardSourceImageOBSScreenshot)(nil)
var _ GetImageBytesFromOBSer = (*DashboardSourceImageOBSScreenshot)(nil)

func (*DashboardSourceImageOBSScreenshot) SourceType() DashboardSourceImageType {
	return DashboardSourceImageTypeOBSScreenshot
}

func obsGetImage(
	ctx context.Context,
	getImageByteser GetImageBytesFromOBSer,
	obsServer obs_grpc.OBSServer,
	el DashboardElementConfig,
	_ *streamtypes.OBSState,
) (image.Image, time.Time, error) {
	b, mimeType, nextUpdateTS, err := getImageByteser.GetImageBytesFromOBS(ctx, obsServer, el)
	if err != nil {
		return nil, nextUpdateTS, fmt.Errorf("unable to get the image from OBS: %w", err)
	}

	var img image.Image
	switch mimeType {
	case "image/png":
		img, err = png.Decode(bytes.NewReader(b))
	case "image/jpeg", "image/jpg":
		img, err = jpeg.Decode(bytes.NewReader(b))
	case "image/webp":
		img, err = webp.Decode(bytes.NewReader(b))
	default:
		return nil, time.Time{}, fmt.Errorf("unexpected MIME type: '%s'", mimeType)
	}
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("unable to parse the image: %w", err)
	}

	return img, nextUpdateTS, nil
}

func (s *DashboardSourceImageOBSScreenshot) GetImageFromOBS(
	ctx context.Context,
	obsServer obs_grpc.OBSServer,
	el DashboardElementConfig,
	obsState *streamtypes.OBSState,
) (image.Image, time.Time, error) {
	return obsGetImage(ctx, s, obsServer, el, obsState)
}

func (s *DashboardSourceImageOBSScreenshot) GetImageBytesFromOBS(
	ctx context.Context,
	obsServer obs_grpc.OBSServer,
	el DashboardElementConfig,
) ([]byte, string, time.Time, error) {
	imageQuality := ptr(int64(el.ImageQuality))
	if s.ImageFormat == ImageFormatJPEG && el.ImageQuality < 100 {
		if s.ImageFormat == ImageFormatJPEG && el.ImageQuality < 50 {
			imageQuality = ptr(int64(el.ImageQuality + 50))
		} else {
			imageQuality = ptr(int64(100))
		}
	}

	if s.Name == "" {
		return nil, "", time.Time{}, fmt.Errorf("empty source name")
	}

	req := &obs_grpc.GetSourceScreenshotRequest{
		SourceName:              &s.Name,
		ImageFormat:             []byte(s.ImageFormat),
		ImageCompressionQuality: imageQuality,
	}
	if s.Width != 0 {
		req.ImageWidth = ptr(int64(s.Width))
	}
	if s.Height != 0 {
		req.ImageHeight = ptr(int64(s.Height))
	}
	if b, err := json.Marshal(req); err == nil {
		logger.Tracef(ctx, "requesting a screenshot from OBS using %s", b)
	}
	resp, err := obsServer.GetSourceScreenshot(ctx, req)
	if err != nil {
		return nil, "", clock.Get().Now().
				Add(time.Second),
			fmt.Errorf(
				"unable to get a screenshot of '%s': %w",
				s.Name,
				err,
			)
	}

	imgB64 := resp.GetImageData()
	imgBytes, mimeType, err := imgb64.Decode(string(imgB64))
	if err != nil {
		return nil, "", time.Time{}, fmt.Errorf(
			"unable to decode the screenshot of '%s': %w",
			s.Name,
			err,
		)
	}

	logger.Tracef(
		ctx,
		"the decoded image is of format '%s' (expected format: '%s') and size %d",
		mimeType,
		s.ImageFormat,
		len(imgBytes),
	)
	return imgBytes, mimeType, clock.Get().Now().Add(time.Duration(s.UpdateInterval)), nil
}

func (s *DashboardSourceImageOBSScreenshot) GetImageBytes(
	ctx context.Context,
	el DashboardElementConfig,
	dataProvider ImageDataProvider,
) ([]byte, string, time.Time, error) {
	obsServer, err := dataProvider.GetOBSServer(ctx)
	if err != nil {
		return nil, "", time.Time{}, fmt.Errorf("unable to get the OBS server: %w", err)
	}
	return s.GetImageBytesFromOBS(ctx, obsServer, el)
}

func (s *DashboardSourceImageOBSScreenshot) GetImage(
	ctx context.Context,
	el DashboardElementConfig,
	dataProvider ImageDataProvider,
) (image.Image, time.Time, error) {
	obsServer, err := dataProvider.GetOBSServer(ctx)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("unable to get the OBS server: %w", err)
	}
	obsState, err := dataProvider.GetOBSState(ctx)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("unable to get the OBS state: %w", err)
	}
	return s.GetImageFromOBS(ctx, obsServer, el, obsState)
}
