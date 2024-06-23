package streampanel

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/facebookincubator/go-belt/tool/logger"
	goyaml "github.com/go-yaml/yaml"
	"github.com/goccy/go-yaml"
	"github.com/nicklaw5/helix/v2"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
)

type ProfileMetadata struct {
	DefaultStreamTitle       string
	DefaultStreamDescription string
	MaxOrder                 int
}

type twitchCache struct {
	Categories []helix.Game
}

type youTubeCache struct {
	Broadcasts []*youtube.LiveBroadcast
}

type gitRepoConfig struct {
	Enable           *bool
	URL              string `yaml:"url,omitempty"`
	PrivateKey       string `yaml:"private_key,omitempty"`
	LatestSyncCommit string `yaml:"latest_sync_commit,omitempty"`
}

type panelData struct {
	Commands struct {
		OnStartStream string `yaml:"on_start_stream"`
		OnStopStream  string `yaml:"on_stop_stream"`
	}
	GitRepo         gitRepoConfig
	Backends        streamcontrol.Config
	ProfileMetadata map[streamcontrol.ProfileName]ProfileMetadata
	Cache           struct {
		Twitch  twitchCache
		Youtube youTubeCache
	} `yaml:"z_cache,omitempty"`
}

func newPanelData() panelData {
	cfg := streamcontrol.Config{}
	obs.InitConfig(cfg)
	twitch.InitConfig(cfg)
	youtube.InitConfig(cfg)
	return panelData{
		Backends:        cfg,
		ProfileMetadata: map[streamcontrol.ProfileName]ProfileMetadata{},
	}
}

func newSamplePanelData() panelData {
	cfg := newPanelData()
	cfg.Backends[obs.ID].StreamProfiles = map[streamcontrol.ProfileName]streamcontrol.AbstractStreamProfile{"some_profile": obs.StreamProfile{}}
	cfg.Backends[twitch.ID].StreamProfiles = map[streamcontrol.ProfileName]streamcontrol.AbstractStreamProfile{"some_profile": twitch.StreamProfile{}}
	cfg.Backends[youtube.ID].StreamProfiles = map[streamcontrol.ProfileName]streamcontrol.AbstractStreamProfile{"some_profile": youtube.StreamProfile{}}
	return cfg
}

var _ = newSamplePanelData

func readPanelDataFromPath(
	ctx context.Context,
	cfgPath string,
	cfg *panelData,
) error {
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		return fmt.Errorf("unable to read file '%s': %w", cfgPath, err)
	}

	return readPanelData(ctx, b, cfg)
}

func readPanelData(
	ctx context.Context,
	b []byte,
	cfg *panelData,
) error {
	err := yaml.Unmarshal(b, cfg)
	if err != nil {
		return fmt.Errorf("unable to unserialize data: %w: <%s>", err, b)
	}

	if cfg.Backends == nil {
		cfg.Backends = streamcontrol.Config{}
	}

	if cfg.Backends[obs.ID] != nil {
		err = streamcontrol.ConvertStreamProfiles[obs.StreamProfile](ctx, cfg.Backends[obs.ID].StreamProfiles)
		if err != nil {
			return fmt.Errorf("unable to convert stream profiles of OBS: %w: <%s>", err, b)
		}
		logger.Debugf(ctx, "final stream profiles of OBS: %#+v", cfg.Backends[obs.ID].StreamProfiles)
	}

	if cfg.Backends[twitch.ID] != nil {
		err = streamcontrol.ConvertStreamProfiles[twitch.StreamProfile](ctx, cfg.Backends[twitch.ID].StreamProfiles)
		if err != nil {
			return fmt.Errorf("unable to convert stream profiles of twitch: %w: <%s>", err, b)
		}
		logger.Debugf(ctx, "final stream profiles of twitch: %#+v", cfg.Backends[twitch.ID].StreamProfiles)
	}

	if cfg.Backends[youtube.ID] != nil {
		err = streamcontrol.ConvertStreamProfiles[youtube.StreamProfile](ctx, cfg.Backends[youtube.ID].StreamProfiles)
		if err != nil {
			return fmt.Errorf("unable to convert stream profiles of youtube: %w: <%s>", err, b)
		}
		logger.Debugf(ctx, "final stream profiles of youtube: %#+v", cfg.Backends[youtube.ID].StreamProfiles)
	}

	return nil
}

func writePanelDataToPath(
	ctx context.Context,
	cfgPath string,
	cfg panelData,
) error {
	pathNew := cfgPath + ".new"
	f, err := os.OpenFile(pathNew, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0750)
	if err != nil {
		return fmt.Errorf("unable to open the data file '%s': %w", pathNew, err)
	}
	err = writePanelData(ctx, f, cfg)
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

func writePanelData(
	_ context.Context,
	w io.Writer,
	cfg panelData,
) error {
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("unable to serialize data %#+v: %w", cfg, err)
	}

	// have to use another YAML encoder to avoid the random-indent bug,
	// but also have to use the initial encoder to correctly map
	// out structures to YAML; so using both sequentially :(

	m := map[string]any{}
	err = goyaml.Unmarshal(b, &m)
	if err != nil {
		return fmt.Errorf("unable to unserialize data %#+v: %w", cfg, err)
	}

	b, err = goyaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("unable to re-serialize data %#+v: %w", cfg, err)
	}

	_, err = io.Copy(w, bytes.NewBuffer(b))
	if err != nil {
		return fmt.Errorf("unable to write data %#+v: %w", cfg, err)
	}
	return nil
}
