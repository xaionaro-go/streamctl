package config

import (
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestDashboardElementConfig(t *testing.T) {
	e := DashboardElementConfig{
		Width:         1,
		Height:        2,
		ZIndex:        3,
		OffsetX:       4,
		OffsetY:       5,
		AlignX:        "a",
		AlignY:        "b",
		Rotate:        6,
		ImageLossless: true,
		ImageQuality:  7,
		Source: &DashboardSourceOBSVideo{
			Name:           "c",
			Width:          8,
			Height:         9,
			ImageFormat:    "d",
			UpdateInterval: 10,
		},
		Filters: []Filter{
			&FilterColor{
				Brightness: 11,
			},
		},
	}

	b, err := yaml.Marshal(e)
	require.NoError(t, err)

	var cmp DashboardElementConfig
	err = yaml.Unmarshal(b, &cmp)
	require.NoError(t, err)

	require.Equal(t, e, cmp)
}
