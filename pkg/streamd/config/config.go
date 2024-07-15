package config

import (
	"context"
	"fmt"
	"os"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	streamserver "github.com/xaionaro-go/streamctl/pkg/streamserver/types"
)

type ProfileMetadata struct {
	DefaultStreamTitle       string
	DefaultStreamDescription string
	MaxOrder                 int
}

type GitRepoConfig struct {
	Enable           *bool
	URL              string `yaml:"url,omitempty"`
	PrivateKey       string `yaml:"private_key,omitempty"`
	LatestSyncCommit string `yaml:"latest_sync_commit,omitempty"` // TODO: deprecate this field, it's just a non-needed mechanism (better to check against git history).
}

type config struct {
	CachePath       *string `yaml:"cache_path"`
	GitRepo         GitRepoConfig
	Backends        streamcontrol.Config
	ProfileMetadata map[streamcontrol.ProfileName]ProfileMetadata
	SentryDSN       string              `yaml:"sentry_dsn"`
	StreamServer    streamserver.Config `yaml:"stream_server"`
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
