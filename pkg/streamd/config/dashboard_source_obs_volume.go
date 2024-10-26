package config

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
	"time"

	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
	"github.com/xaionaro-go/streamctl/pkg/colorx"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
)

type DashboardSourceOBSVolume struct {
	Name           string   `yaml:"name"            json:"name"`
	UpdateInterval Duration `yaml:"update_interval" json:"update_interval"`
	ColorActive    string   `yaml:"color_active"    json:"color_active"`
	ColorPassive   string   `yaml:"color_passive"   json:"color_passive"`
}

var _ Source = (*DashboardSourceOBSVolume)(nil)

func (s *DashboardSourceOBSVolume) GetImage(
	ctx context.Context,
	obsServer obs_grpc.OBSServer,
	el DashboardElementConfig,
	obsState *streamtypes.OBSState,
) (image.Image, time.Time, error) {
	if obsState == nil {
		return nil, time.Time{}, fmt.Errorf("obsState == nil")
	}
	volumeMeters := xsync.DoR1(ctx, &obsState.Mutex, func() [][3]float64 {
		return obsState.VolumeMeters[s.Name]
	})

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
		return nil, time.Time{}, fmt.Errorf(
			"unable to parse the `color_active` value '%s': %w",
			s.ColorActive,
			err,
		)
	}
	colorPassive, err := colorx.Parse(s.ColorPassive)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf(
			"unable to parse the `color_passive` value '%s': %w",
			s.ColorPassive,
			err,
		)
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

func (*DashboardSourceOBSVolume) SourceType() DashboardSourceType {
	return DashboardSourceTypeOBSVolume
}
