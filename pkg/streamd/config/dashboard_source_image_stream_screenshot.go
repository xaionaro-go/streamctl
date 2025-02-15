package config

import (
	"context"
	"fmt"
	"image"
	"time"
)

type DashboardSourceImageStreamScreenshot struct {
	Name           string   `yaml:"name"            json:"name"`
	Width          float64  `yaml:"width"           json:"width"`
	Height         float64  `yaml:"height"          json:"height"`
	UpdateInterval Duration `yaml:"update_interval" json:"update_interval"`
}

var _ SourceImage = (*DashboardSourceImageOBSScreenshot)(nil)
var _ GetImageBytesFromOBSer = (*DashboardSourceImageOBSScreenshot)(nil)

func (*DashboardSourceImageStreamScreenshot) SourceType() DashboardSourceImageType {
	return DashboardSourceImageTypeStreamScreenshot
}

func (s *DashboardSourceImageStreamScreenshot) GetImage(
	ctx context.Context,
	el DashboardElementConfig,
	dp ImageDataProvider,
) (image.Image, time.Time, error) {
	return nil, time.Time{}, fmt.Errorf("not implemented")
}

func (s *DashboardSourceImageStreamScreenshot) GetImageBytes(
	ctx context.Context,
	el DashboardElementConfig,
) ([]byte, string, time.Time, error) {
	return nil, "", time.Time{}, fmt.Errorf("not implemented")
}
