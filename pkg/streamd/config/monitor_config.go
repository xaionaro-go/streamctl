package config

import (
	"fmt"
	"runtime/debug"

	"github.com/goccy/go-yaml"
	"github.com/xaionaro-go/streamctl/pkg/streamd/consts"
)

type AlignX = consts.AlignX
type AlignY = consts.AlignY

type MonitorConfig struct {
	Elements map[string]MonitorElementConfig
}

type MonitorElementConfig _RawMonitorElementConfig

type _RawMonitorElementConfig struct {
	Width         float64  `yaml:"width"`
	Height        float64  `yaml:"height"`
	ZIndex        float64  `yaml:"z_index"`
	OffsetX       float64  `yaml:"offset_x"`
	OffsetY       float64  `yaml:"offset_y"`
	AlignX        AlignX   `yaml:"align_x"`
	AlignY        AlignY   `yaml:"align_y"`
	Rotate        float64  `yaml:"rotate"`
	ImageLossless bool     `yaml:"image_lossless"`
	ImageQuality  float64  `yaml:"image_quality"`
	Source        Source   `yaml:"source"`
	Filters       []Filter `yaml:"filters"`
}

type serializableMonitorElementConfig struct {
	Width         float64              `yaml:"width"`
	Height        float64              `yaml:"height"`
	ZIndex        float64              `yaml:"z_index"`
	OffsetX       float64              `yaml:"offset_x"`
	OffsetY       float64              `yaml:"offset_y"`
	AlignX        AlignX               `yaml:"align_x"`
	AlignY        AlignY               `yaml:"align_y"`
	Rotate        float64              `yaml:"rotate"`
	ImageLossless bool                 `yaml:"image_lossless"`
	ImageQuality  float64              `yaml:"image_quality"`
	Source        serializableSource   `yaml:"source"`
	Filters       []serializableFilter `yaml:"filters"`
}

func (cfg *MonitorElementConfig) UnmarshalYAML(b []byte) (_err error) {
	defer func() {
		if r := recover(); r != nil {
			_err = fmt.Errorf("got a panic: %v\n%s", r, debug.Stack())
		}
	}()
	if cfg == nil {
		return fmt.Errorf("nil MonitorElementConfig")
	}

	intermediate := serializableMonitorElementConfig{}
	err := yaml.Unmarshal(b, &intermediate)
	if err != nil {
		return fmt.Errorf("unable to unmarshal the MonitorElementConfig: %w", err)
	}

	*cfg = MonitorElementConfig{
		Width:         intermediate.Width,
		Height:        intermediate.Height,
		ZIndex:        intermediate.ZIndex,
		OffsetX:       intermediate.OffsetX,
		OffsetY:       intermediate.OffsetY,
		AlignX:        intermediate.AlignX,
		AlignY:        intermediate.AlignY,
		Rotate:        intermediate.Rotate,
		ImageLossless: intermediate.ImageLossless,
		ImageQuality:  intermediate.ImageQuality,
	}

	cfg.Source = intermediate.Source.Unwrap()
	cfg.Filters = make([]Filter, 0, len(intermediate.Filters))
	for _, filter := range intermediate.Filters {
		cfg.Filters = append(cfg.Filters, filter.Unwrap())
	}
	return nil
}

func (cfg MonitorElementConfig) MarshalYAML() (b []byte, _err error) {
	defer func() {
		if r := recover(); r != nil {
			_err = fmt.Errorf("got a panic: %v\n%s", r, debug.Stack())
		}
	}()

	intermediate := &serializableMonitorElementConfig{
		Width:         cfg.Width,
		Height:        cfg.Height,
		ZIndex:        cfg.ZIndex,
		OffsetX:       cfg.OffsetX,
		OffsetY:       cfg.OffsetY,
		AlignX:        cfg.AlignX,
		AlignY:        cfg.AlignY,
		Rotate:        cfg.Rotate,
		ImageLossless: cfg.ImageLossless,
		ImageQuality:  cfg.ImageQuality,
		Source:        wrapSourceForYaml(cfg.Source),
		Filters:       make([]serializableFilter, 0, len(cfg.Filters)),
	}
	for _, filter := range cfg.Filters {
		intermediate.Filters = append(intermediate.Filters, wrapFilterForYaml(filter))
	}

	return yaml.Marshal(intermediate)
}
