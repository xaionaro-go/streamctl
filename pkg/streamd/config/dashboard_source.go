package config

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"time"

	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

type DashboardSourceType string

const (
	DashboardSourceTypeUndefined = DashboardSourceType("")
	DashboardSourceTypeDummy     = DashboardSourceType("dummy")
	DashboardSourceTypeOBSVideo  = DashboardSourceType("obs_video")
	DashboardSourceTypeOBSVolume = DashboardSourceType("obs_volume")
)

func (mst DashboardSourceType) New() Source {
	switch mst {
	case DashboardSourceTypeDummy:
		return &DashboardSourceDummy{}
	case DashboardSourceTypeOBSVideo:
		return &DashboardSourceOBSVideo{}
	case DashboardSourceTypeOBSVolume:
		return &DashboardSourceOBSVolume{}
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
		el DashboardElementConfig,
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

type DashboardSourceDummy struct{}

func (*DashboardSourceDummy) GetImage(
	ctx context.Context,
	obsServer obs_grpc.OBSServer,
	el DashboardElementConfig,
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

func (*DashboardSourceDummy) SourceType() DashboardSourceType {
	return DashboardSourceTypeDummy
}

var _ Source = (*DashboardSourceDummy)(nil)

type Source interface {
	GetImage(
		ctx context.Context,
		obsServer obs_grpc.OBSServer,
		el DashboardElementConfig,
		obsState *streamtypes.OBSState,
	) (image.Image, time.Time, error)

	SourceType() DashboardSourceType
}

type serializableSource struct {
	Type   DashboardSourceType `yaml:"type"`
	Config map[string]any      `yaml:"config,omitempty"`
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
