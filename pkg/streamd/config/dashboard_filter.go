package config

import (
	"context"
	"image"
	"image/color"

	"github.com/anthonynsimon/bild/adjust"
)

type DashboardFilterType string

const (
	UndefinedDashboardFilterType = DashboardFilterType("")
	DashboardFilterTypeColor     = DashboardFilterType("color")
)

func (mst DashboardFilterType) New() Filter {
	switch mst {
	case DashboardFilterTypeColor:
		return &FilterColor{}
	default:
		return nil
	}
}

type Filter interface {
	Filter(context.Context, image.Image) image.Image
	DashboardFilterType() DashboardFilterType
}

type FilterColor struct {
	Brightness float64 `yaml:"brightness" json:"brightness"`
	Opacity    float64 `yaml:"opacity"    json:"opacity"`
}

func (f *FilterColor) Filter(
	ctx context.Context,
	img image.Image,
) image.Image {
	if f.Brightness != 0 {
		img = adjust.Brightness(img, f.Brightness)
	}
	if f.Opacity != 0 {
		img = processImage(img, func(pixel color.Color) color.Color {
			switch pixel := pixel.(type) {
			case color.RGBA:
				pixel.A = uint8(float64(pixel.A) * f.Opacity)
				return pixel
			default:
				r, g, b, a := pixel.RGBA()
				a = uint32(float64(a) * f.Opacity)
				return color.RGBA{
					R: uint8(r),
					G: uint8(g),
					B: uint8(b),
					A: uint8(a),
				}
			}
		})
	}
	return img
}

func processImage(
	img image.Image,
	pixelCallback func(pixel color.Color) color.Color,
) image.Image {
	size := img.Bounds().Size()
	result := image.NewRGBA(image.Rectangle{
		Min: image.Point{
			X: 0,
			Y: 0,
		},
		Max: size,
	})
	for x := 0; x < size.X; x++ {
		for y := 0; y < size.Y; y++ {
			pixel := img.At(x, y)
			result.Set(x, y, pixelCallback(pixel))
		}
	}
	return result
}

func (f *FilterColor) DashboardFilterType() DashboardFilterType {
	return DashboardFilterTypeColor
}

type serializableFilter struct {
	Type   DashboardFilterType `yaml:"type"             json:"type"`
	Config map[string]any      `yaml:"config,omitempty" json:"config"`
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
		Type:   filter.DashboardFilterType(),
		Config: toMap(filter),
	}
}
