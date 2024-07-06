package config

import (
	"context"
	"fmt"
	"io"

	"github.com/goccy/go-yaml"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
)

var _ io.Reader = (*Config)(nil)
var _ io.ReaderFrom = (*Config)(nil)
var _ yaml.BytesUnmarshaler = (*Config)(nil)

func (cfg *Config) Read(
	b []byte,
) (int, error) {
	return len(b), cfg.UnmarshalYAML(b)
}

func (cfg *Config) UnmarshalYAML(b []byte) error {
	err := yaml.Unmarshal(b, (*config)(cfg))
	if err != nil {
		return fmt.Errorf("unable to unserialize data: %w", err)
	}

	if cfg.Backends == nil {
		cfg.Backends = streamcontrol.Config{}
	}

	if cfg.Backends[obs.ID] != nil {
		err = streamcontrol.ConvertStreamProfiles[obs.StreamProfile](context.Background(), cfg.Backends[obs.ID].StreamProfiles)
		if err != nil {
			return fmt.Errorf("unable to convert stream profiles of OBS: %w", err)
		}
	}

	if cfg.Backends[twitch.ID] != nil {
		err = streamcontrol.ConvertStreamProfiles[twitch.StreamProfile](context.Background(), cfg.Backends[twitch.ID].StreamProfiles)
		if err != nil {
			return fmt.Errorf("unable to convert stream profiles of twitch: %w", err)
		}
	}

	if cfg.Backends[youtube.ID] != nil {
		err = streamcontrol.ConvertStreamProfiles[youtube.StreamProfile](context.Background(), cfg.Backends[youtube.ID].StreamProfiles)
		if err != nil {
			return fmt.Errorf("unable to convert stream profiles of youtube: %w", err)
		}
	}

	return nil
}

func (cfg *Config) ReadFrom(
	r io.Reader,
) (int64, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return int64(len(b)), fmt.Errorf("unable to read: %w", err)
	}

	n, err := cfg.Read(b)
	return int64(n), err
}
