package config

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
	"github.com/xaionaro-go/streamctl/pkg/imgb64"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

type DashboardSourceOBSVideo struct {
	Name           string      `yaml:"name"            json:"name"`
	Width          float64     `yaml:"width"           json:"width"`
	Height         float64     `yaml:"height"          json:"height"`
	ImageFormat    ImageFormat `yaml:"image_format"    json:"image_format"`
	UpdateInterval Duration    `yaml:"update_interval" json:"update_interval"`
}

var _ Source = (*DashboardSourceOBSVideo)(nil)
var _ GetImageByteser = (*DashboardSourceOBSVideo)(nil)

func (*DashboardSourceOBSVideo) SourceType() DashboardSourceType {
	return DashboardSourceTypeOBSVideo
}

func (s *DashboardSourceOBSVideo) GetImage(
	ctx context.Context,
	obsServer obs_grpc.OBSServer,
	el DashboardElementConfig,
	obsState *streamtypes.OBSState,
) (image.Image, time.Time, error) {
	return obsGetImage(ctx, s, obsServer, el, obsState)
}

func (s *DashboardSourceOBSVideo) GetImageBytes(
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
		return nil, "", time.Now().
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
	return imgBytes, mimeType, time.Now().Add(time.Duration(s.UpdateInterval)), nil
}
