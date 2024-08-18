package config

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd/consts"
	streamserver "github.com/xaionaro-go/streamctl/pkg/streamserver/types"
)

type AlignX = consts.AlignX
type AlignY = consts.AlignY

type ImageFormat string

const (
	ImageFormatUndefined = ImageFormat("")
	ImageFormatPNG       = ImageFormat("png")
	ImageFormatJPEG      = ImageFormat("jpeg")
	ImageFormatWebP      = ImageFormat("webp")
)

type OBSSource struct {
	SourceName     string      `yaml:"source_name"`
	SourceWidth    float64     `yaml:"source_width"`
	SourceHeight   float64     `yaml:"source_height"`
	DisplayWidth   float64     `yaml:"display_width"`
	DisplayHeight  float64     `yaml:"display_height"`
	ZIndex         float64     `yaml:"z_index"`
	OffsetX        float64     `yaml:"offset_x"`
	OffsetY        float64     `yaml:"offset_y"`
	AlignX         AlignX      `yaml:"align_x"`
	AlignY         AlignY      `yaml:"align_y"`
	ImageFormat    ImageFormat `yaml:"image_format"`
	ImageQuality   float64     `yaml:"image_quality"`
	Rotate         float64
	UpdateInterval time.Duration `yaml:"update_interval"`
}

type MonitorConfig struct {
	Elements map[string]OBSSource
}

type ProfileMetadata struct {
	DefaultStreamTitle       string
	DefaultStreamDescription string
	MaxOrder                 int
}

type config struct {
	CachePath       *string `yaml:"cache_path"`
	GitRepo         GitRepoConfig
	Backends        streamcontrol.Config
	ProfileMetadata map[streamcontrol.ProfileName]ProfileMetadata
	StreamServer    streamserver.Config `yaml:"stream_server"`
	Monitor         MonitorConfig
}

type Config config

func NewConfig() Config {
	cfg := streamcontrol.Config{}
	obs.InitConfig(cfg)
	twitch.InitConfig(cfg)
	youtube.InitConfig(cfg)
	return Config{
		Backends:        cfg,
		ProfileMetadata: map[streamcontrol.ProfileName]ProfileMetadata{},
		CachePath:       ptr("~/.streamd.cache"),
		Monitor: MonitorConfig{
			Elements: map[string]OBSSource{},
		},
	}
}

func NewSampleConfig() Config {
	cfg := NewConfig()
	cfg.Backends[obs.ID].StreamProfiles = map[streamcontrol.ProfileName]streamcontrol.AbstractStreamProfile{"some_profile": obs.StreamProfile{}}
	cfg.Backends[twitch.ID].StreamProfiles = map[streamcontrol.ProfileName]streamcontrol.AbstractStreamProfile{"some_profile": twitch.StreamProfile{}}
	cfg.Backends[youtube.ID].StreamProfiles = map[streamcontrol.ProfileName]streamcontrol.AbstractStreamProfile{"some_profile": youtube.StreamProfile{}}
	return cfg
}

var _ = NewSampleConfig

func ReadConfigFromPath(
	ctx context.Context,
	cfgPath string,
	cfg *Config,
) error {
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		return fmt.Errorf("unable to read file '%s': %w", cfgPath, err)
	}

	_, err = cfg.Read(b)
	return err
}

func ReadOrCreateConfigFile(
	ctx context.Context,
	dataPath string,
) (*Config, error) {
	_, err := os.Stat(dataPath)
	switch {
	case err == nil:
		data := Config{}
		err := ReadConfigFromPath(ctx, dataPath, &data)
		if err != nil {
			return nil, fmt.Errorf("unable to read panel data from path '%s': %w", dataPath, err)
		}
		return &data, nil
	case os.IsNotExist(err):
		logger.Debugf(ctx, "cannot find file '%s', creating", dataPath)
		data := NewConfig()
		err := WriteConfigToPath(ctx, dataPath, data)
		if err != nil {
			logger.Errorf(ctx, "unable to write config to path '%s': %w", dataPath, err)
		}
		return &data, nil
	default:
		return nil, fmt.Errorf("unable to access file '%s': %w", dataPath, err)
	}
}

func WriteConfigToPath(
	ctx context.Context,
	cfgPath string,
	cfg Config,
) error {
	pathNew := cfgPath + ".new"
	f, err := os.OpenFile(pathNew, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0750)
	if err != nil {
		return fmt.Errorf("unable to open the data file '%s': %w", pathNew, err)
	}
	_, err = cfg.WriteTo(f)
	f.Close()
	if err != nil {
		return fmt.Errorf("unable to write data to file '%s': %w", pathNew, err)
	}
	err = os.Rename(pathNew, cfgPath)
	if err != nil {
		return fmt.Errorf("cannot move '%s' to '%s': %w", pathNew, cfgPath, err)
	}
	logger.Infof(ctx, "wrote to '%s' config %#+v", cfgPath, cfg)
	return nil
}
