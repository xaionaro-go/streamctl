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

type DashboardSourceImageType string

const (
	DashboardSourceImageTypeUndefined = DashboardSourceImageType("")
	DashboardSourceImageTypeDummy     = DashboardSourceImageType("dummy")
	DashboardSourceImageTypeOBSVideo  = DashboardSourceImageType("obs_video") // rename to `obs_screenshot`
	DashboardSourceImageTypeOBSVolume = DashboardSourceImageType("obs_volume")
)

func (mst DashboardSourceImageType) New() SourceImage {
	switch mst {
	case DashboardSourceImageTypeDummy:
		return &DashboardSourceImageDummy{}
	case DashboardSourceImageTypeOBSVideo:
		return &DashboardSourceImageOBSScreenshot{}
	case DashboardSourceImageTypeOBSVolume:
		return &DashboardSourceImageOBSVolume{}
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

var _ SourceImage = (*DashboardSourceImageDummy)(nil)

type SourceImage interface {
	GetImage(
		ctx context.Context,
		obsServer obs_grpc.OBSServer,
		el DashboardElementConfig,
		obsState *streamtypes.OBSState,
	) (image.Image, time.Time, error)

	SourceType() DashboardSourceImageType
}

type serializableSourceImage struct {
	Type   DashboardSourceImageType `yaml:"type"`
	Config map[string]any           `yaml:"config,omitempty"`
}

func (s serializableSourceImage) Unwrap() SourceImage {
	result := s.Type.New()
	fromMap(s.Config, &result)
	return result
}

func wrapSourceImageForYaml(source SourceImage) serializableSourceImage {
	return serializableSourceImage{
		Type:   source.SourceType(),
		Config: toMap(source),
	}
}
