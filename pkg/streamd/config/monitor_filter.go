package config

import (
	"context"
	"image"

	"github.com/anthonynsimon/bild/adjust"
)

type MonitorFilterType string

const (
	MonitorFilterTypeUndefined = MonitorFilterType("")
	MonitorFilterTypeColor     = MonitorFilterType("color")
)

func (mst MonitorFilterType) New() Filter {
	switch mst {
	case MonitorFilterTypeColor:
		return &FilterColor{}
	default:
		return nil
	}
}

type Filter interface {
	Filter(context.Context, image.Image) image.Image
	MonitorFilterType() MonitorFilterType
}

type FilterColor struct {
	Brightness float64
}

func (f *FilterColor) Filter(
	ctx context.Context,
	img image.Image,
) image.Image {
	if f.Brightness != 0 {
		img = adjust.Brightness(img, f.Brightness)
	}
	return img
}

func (f *FilterColor) MonitorFilterType() MonitorFilterType {
	return MonitorFilterTypeColor
}

type serializableFilter struct {
	Type   MonitorFilterType `yaml:"type"`
	Config map[string]any    `yaml:"config,omitempty"`
}

func (s serializableFilter) Unwrap() Filter {
	result := s.Type.New()
	fromMap(s.Config, &result)
	return result
}

func (s serializableFilter) Filter(
	ctx context.Context,
	img image.Image,
) image.Image {
	return nil
}

func wrapFilterForYaml(filter Filter) serializableFilter {
	return serializableFilter{
		Type:   filter.MonitorFilterType(),
		Config: toMap(filter),
	}
}
