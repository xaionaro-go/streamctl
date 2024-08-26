package config

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"time"

	"github.com/chai2010/webp"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
	"github.com/xaionaro-go/streamctl/pkg/colorx"
	"github.com/xaionaro-go/streamctl/pkg/imgb64"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

type MonitorSourceType string

const (
	MonitorSourceTypeUndefined = MonitorSourceType("")
	MonitorSourceTypeDummy     = MonitorSourceType("dummy")
	MonitorSourceTypeOBSVideo  = MonitorSourceType("obs_video")
	MonitorSourceTypeOBSVolume = MonitorSourceType("obs_volume")
)

func (mst MonitorSourceType) New() Source {
	switch mst {
	case MonitorSourceTypeDummy:
		return &MonitorSourceDummy{}
	case MonitorSourceTypeOBSVideo:
		return &MonitorSourceOBSVideo{}
	case MonitorSourceTypeOBSVolume:
		return &MonitorSourceOBSVolume{}
	default:
		return nil
	}
}

type ImageFormat string

const (
	ImageFormatUndefined = ImageFormat("")
	ImageFormatPNG       = ImageFormat("png")
	ImageFormatJPEG      = ImageFormat("jpeg")
	ImageFormatWebP      = ImageFormat("webp")
)

type GetImageByteser interface {
	GetImageBytes(
		ctx context.Context,
		obsServer obs_grpc.OBSServer,
		el MonitorElementConfig,
	) ([]byte, string, time.Time, error)
}

type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var value any
	if err := json.Unmarshal(b, &value); err != nil {
		return fmt.Errorf("unable to un-JSON-ize '%s': %w", b, err)
	}

	switch value := value.(type) {
	case float64:
		*d = Duration(value)
		return nil
	case string:
		duration, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("unable to parse '%s' as duration: %w", value, err)
		}
		*d = Duration(duration)
		return nil
	default:
		return fmt.Errorf("unexpected type: %T", value)
	}
}

type MonitorSourceOBSVideo struct {
	Name           string      `yaml:"name" json:"name"`
	Width          float64     `yaml:"width" json:"width"`
	Height         float64     `yaml:"height" json:"height"`
	ImageFormat    ImageFormat `yaml:"image_format" json:"image_format"`
	UpdateInterval Duration    `yaml:"update_interval" json:"update_interval"`
}

var _ Source = (*MonitorSourceOBSVideo)(nil)
var _ GetImageByteser = (*MonitorSourceOBSVideo)(nil)

func (*MonitorSourceOBSVideo) SourceType() MonitorSourceType {
	return MonitorSourceTypeOBSVideo
}

func (s *MonitorSourceOBSVideo) GetImage(
	ctx context.Context,
	obsServer obs_grpc.OBSServer,
	el MonitorElementConfig,
	obsState *streamtypes.OBSState,
) (image.Image, time.Time, error) {
	b, mimeType, nextUpdateTS, err := s.GetImageBytes(ctx, obsServer, el)
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

func (s *MonitorSourceOBSVideo) GetImageBytes(
	ctx context.Context,
	obsServer obs_grpc.OBSServer,
	el MonitorElementConfig,
) ([]byte, string, time.Time, error) {
	imageQuality := ptr(int64(el.ImageQuality))
	if s.ImageFormat == ImageFormatJPEG && el.ImageQuality < 100 {
		if s.ImageFormat == ImageFormatJPEG && el.ImageQuality < 50 {
			imageQuality = ptr(int64(el.ImageQuality + 50))
		} else {
			imageQuality = ptr(int64(100))
		}
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
		return nil, "", time.Now().Add(time.Second), fmt.Errorf("unable to get a screenshot of '%s': %w", s.Name, err)
	}

	imgB64 := resp.GetImageData()
	imgBytes, mimeType, err := imgb64.Decode(string(imgB64))
	if err != nil {
		return nil, "", time.Time{}, fmt.Errorf("unable to decode the screenshot of '%s': %w", s.Name, err)
	}

	logger.Tracef(ctx, "the decoded image is of format '%s' (expected format: '%s') and size %d", mimeType, s.ImageFormat, len(imgBytes))
	return imgBytes, mimeType, time.Now().Add(time.Duration(s.UpdateInterval)), nil
}

type MonitorSourceOBSVolume struct {
	Name           string   `yaml:"name" json:"name"`
	UpdateInterval Duration `yaml:"update_interval" json:"update_interval"`
	ColorActive    string   `yaml:"color_active" json:"color_active"`
	ColorPassive   string   `yaml:"color_passive" json:"color_passive"`
}

var _ Source = (*MonitorSourceOBSVolume)(nil)

func (s *MonitorSourceOBSVolume) GetImage(
	ctx context.Context,
	obsServer obs_grpc.OBSServer,
	el MonitorElementConfig,
	obsState *streamtypes.OBSState,
) (image.Image, time.Time, error) {
	if obsState == nil {
		return nil, time.Time{}, fmt.Errorf("obsState == nil")
	}
	obsState.Lock()
	volumeMeters := obsState.VolumeMeters[s.Name]
	obsState.Unlock()

	if len(volumeMeters) == 0 {
		return nil, time.Now().Add(time.Second), fmt.Errorf("no data for volume of '%s'", s.Name)
	}
	var volume float64
	for _, s := range volumeMeters {
		for _, cmp := range s {
			volume = math.Max(volume, cmp)
		}
	}

	img := image.NewRGBA(image.Rectangle{
		Min: image.Point{
			X: 0,
			Y: 0,
		},
		Max: image.Point{
			X: int(el.Width),
			Y: int(el.Height),
		},
	})

	colorActive, err := colorx.Parse(s.ColorActive)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("unable to parse the `color_active` value '%s': %w", s.ColorActive, err)
	}
	colorPassive, err := colorx.Parse(s.ColorPassive)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("unable to parse the `color_passive` value '%s': %w", s.ColorPassive, err)
	}

	size := img.Bounds().Size()
	for x := 0; x < size.X; x++ {
		volumeExpected := float64(x+1) / float64(size.X)
		var c color.Color
		if volumeExpected <= volume {
			c = colorActive
		} else {
			c = colorPassive
		}
		for y := 0; y < size.Y; y++ {
			img.Set(x, y, c)
		}
	}

	return img, time.Now().Add(time.Duration(s.UpdateInterval)), nil
}

func (*MonitorSourceOBSVolume) SourceType() MonitorSourceType {
	return MonitorSourceTypeOBSVolume
}

type MonitorSourceDummy struct{}

func (*MonitorSourceDummy) GetImage(
	ctx context.Context,
	obsServer obs_grpc.OBSServer,
	el MonitorElementConfig,
	obsState *streamtypes.OBSState,
) (image.Image, time.Time, error) {
	img := image.NewRGBA(image.Rectangle{
		Min: image.Point{
			X: 0,
			Y: 0,
		},
		Max: image.Point{
			X: 0,
			Y: 0,
		},
	})
	return img, time.Time{}, nil
}

func (*MonitorSourceDummy) SourceType() MonitorSourceType {
	return MonitorSourceTypeDummy
}

var _ Source = (*MonitorSourceDummy)(nil)

type Source interface {
	GetImage(
		ctx context.Context,
		obsServer obs_grpc.OBSServer,
		el MonitorElementConfig,
		obsState *streamtypes.OBSState,
	) (image.Image, time.Time, error)

	SourceType() MonitorSourceType
}

type serializableSource struct {
	Type   MonitorSourceType `yaml:"type"`
	Config map[string]any    `yaml:"config,omitempty"`
}

func (s serializableSource) Unwrap() Source {
	result := s.Type.New()
	fromMap(s.Config, &result)
	return result
}

func wrapSourceForYaml(source Source) serializableSource {
	return serializableSource{
		Type:   source.SourceType(),
		Config: toMap(source),
	}
}
