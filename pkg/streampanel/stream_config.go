package streampanel

import (
	"context"
	"fmt"
	"os"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
	"github.com/spf13/cobra"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
)

type streamConfig struct {
	ControllersConfig streamcontrol.Config
}

func newStreamConfig() streamConfig {
	cfg := streamcontrol.Config{}
	twitch.InitConfig(cfg)
	youtube.InitConfig(cfg)
	return streamConfig{
		ControllersConfig: cfg,
	}
}

func newSampleStreamConfig(cmd *cobra.Command, args []string) streamConfig {
	cfg := newStreamConfig()
	cfg.ControllersConfig[twitch.ID].StreamProfiles = map[streamcontrol.ProfileName]streamcontrol.AbstractStreamProfile{"some_profile": twitch.StreamProfile{}}
	cfg.ControllersConfig[youtube.ID].StreamProfiles = map[streamcontrol.ProfileName]streamcontrol.AbstractStreamProfile{"some_profile": youtube.StreamProfile{}}
	return cfg
}

func readConfigFromPath(
	ctx context.Context,
	cfgPath string,
	cfg *streamConfig,
) error {
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		return fmt.Errorf("unable to read file '%s': %w", cfgPath, err)
	}

	err = yaml.Unmarshal(b, cfg)
	if err != nil {
		return fmt.Errorf("unable to unserialize config: %w: <%s>", err, b)
	}

	err = streamcontrol.ConvertStreamProfiles[twitch.StreamProfile](ctx, cfg.ControllersConfig[twitch.ID].StreamProfiles)
	if err != nil {
		return fmt.Errorf("unable to convert stream profiles of twitch: %w: <%s>", err, b)
	}
	logger.Debugf(ctx, "final stream profiles of twitch: %#+v", cfg.ControllersConfig[twitch.ID].StreamProfiles)

	err = streamcontrol.ConvertStreamProfiles[youtube.StreamProfile](ctx, cfg.ControllersConfig[youtube.ID].StreamProfiles)
	if err != nil {
		return fmt.Errorf("unable to convert stream profiles of twitch: %w: <%s>", err, b)
	}
	logger.Debugf(ctx, "final stream profiles of youtube: %#+v", cfg.ControllersConfig[youtube.ID].StreamProfiles)

	return nil
}

func writeConfigToPath(
	ctx context.Context,
	cfgPath string,
	cfg streamConfig,
) error {
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("unable to serialize config %#+v: %w", cfg, err)
	}
	pathNew := cfgPath + ".new"
	err = os.WriteFile(pathNew, b, 0750)
	if err != nil {
		return fmt.Errorf("unable to write config to file '%s': %w", pathNew, err)
	}
	err = os.Rename(pathNew, cfgPath)
	if err != nil {
		return fmt.Errorf("cannot move '%s' to '%s': %w", pathNew, cfgPath, err)
	}
	logger.Infof(ctx, "wrote to '%s' config <%s>", cfgPath, b)
	return nil
}
