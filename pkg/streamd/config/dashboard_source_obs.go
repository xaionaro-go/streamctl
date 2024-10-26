package config

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"time"

	"github.com/chai2010/webp"
	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

func obsGetImage(
	ctx context.Context,
	getImageByteser GetImageByteser,
	obsServer obs_grpc.OBSServer,
	el DashboardElementConfig,
	obsState *streamtypes.OBSState,
) (image.Image, time.Time, error) {
	b, mimeType, nextUpdateTS, err := getImageByteser.GetImageBytes(ctx, obsServer, el)
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
