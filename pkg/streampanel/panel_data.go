package streampanel

import (
	"context"
	"fmt"
	"os"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
	"github.com/nicklaw5/helix/v2"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
)

type ProfileMetadata struct {
	DefaultStreamTitle       string
	DefaultStreamDescription string
	MaxOrder                 int
}

type panelData struct {
	Backends        streamcontrol.Config
	ProfileMetadata map[streamcontrol.ProfileName]ProfileMetadata
	Cache           struct {
		Twitch struct {
			Categories []helix.Game
		}
		Youtube struct {
			Broadcasts []*youtube.LiveBroadcast
		}
	}
}

func newPanelData() panelData {
	cfg := streamcontrol.Config{}
	twitch.InitConfig(cfg)
	youtube.InitConfig(cfg)
	return panelData{
		Backends:        cfg,
		ProfileMetadata: map[streamcontrol.ProfileName]ProfileMetadata{},
	}
}

func newSamplePanelData() panelData {
	cfg := newPanelData()
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

	err = yaml.Unmarshal(b, cfg)
	if err != nil {
		return fmt.Errorf("unable to unserialize data: %w: <%s>", err, b)
	}

	if cfg.Backends == nil {
		cfg.Backends = streamcontrol.Config{}
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
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("unable to serialize data %#+v: %w", cfg, err)
	}
	pathNew := cfgPath + ".new"
	err = os.WriteFile(pathNew, b, 0750)
	if err != nil {
		return fmt.Errorf("unable to write data to file '%s': %w", pathNew, err)
	}
	err = os.Rename(pathNew, cfgPath)
	if err != nil {
		return fmt.Errorf("cannot move '%s' to '%s': %w", pathNew, cfgPath, err)
	}
	logger.Infof(ctx, "wrote to '%s' data <%s>", cfgPath, b)
	return nil
}
