package streamd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	streamd "github.com/xaionaro-go/streamctl/pkg/streamd/types"
)

func (d *StreamD) EXPERIMENTAL_ReinitStreamControllers(ctx context.Context) error {
	platNames := make([]streamcontrol.PlatformName, 0, len(d.Config.Backends))
	for platName := range d.Config.Backends {
		platNames = append(platNames, platName)
	}
	sort.Slice(platNames, func(i, j int) bool {
		return platNames[i] < platNames[j]
	})
	for _, platName := range platNames {
		var err error
		switch strings.ToLower(string(platName)) {
		case strings.ToLower(string(obs.ID)):
			err = d.initOBSBackend(ctx)
		case strings.ToLower(string(twitch.ID)):
			err = d.initTwitchBackend(ctx)
		case strings.ToLower(string(youtube.ID)):
			err = d.initYouTubeBackend(ctx)
		}
		if err != nil && err != ErrSkipBackend {
			return fmt.Errorf("unable to initialize '%s': %w", platName, err)
		}
	}
	return nil
}

var ErrSkipBackend = streamd.ErrSkipBackend

func newOBS(
	ctx context.Context,
	cfg *streamcontrol.AbstractPlatformConfig,
	setConnectionInfo func(context.Context, *streamcontrol.PlatformConfig[obs.PlatformSpecificConfig, obs.StreamProfile]) (bool, error),
	saveCfgFunc func(*streamcontrol.AbstractPlatformConfig) error,
) (
	*obs.OBS,
	error,
) {
	platCfg := streamcontrol.ConvertPlatformConfig[obs.PlatformSpecificConfig, obs.StreamProfile](
		ctx, cfg,
	)
	if platCfg == nil {
		return nil, fmt.Errorf("OBS config was not found")
	}

	if cfg.Enable != nil && !*cfg.Enable {
		return nil, ErrSkipBackend
	}

	hadSetNewConnectionInfo := false
	if platCfg.Config.Host == "" || platCfg.Config.Port == 0 {
		ok, err := setConnectionInfo(ctx, platCfg)
		if !ok {
			err := saveCfgFunc(&streamcontrol.AbstractPlatformConfig{
				Enable:         platCfg.Enable,
				Config:         platCfg.Config,
				StreamProfiles: streamcontrol.ToAbstractStreamProfiles(platCfg.StreamProfiles),
			})
			if err != nil {
				logger.Error(ctx, err)
			}
			return nil, ErrSkipBackend
		}
		if err != nil {
			return nil, fmt.Errorf("unable to set connection info: %w", err)
		}
		hadSetNewConnectionInfo = true
	}

	logger.Debugf(ctx, "OBS config: %#+v", platCfg)
	cfg = streamcontrol.ToAbstractPlatformConfig(ctx, platCfg)
	obs, err := obs.New(ctx, *platCfg)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize OBS client: %w", err)
	}
	if hadSetNewConnectionInfo {
		logger.Debugf(ctx, "confirmed new OBS connection info, saving it")
		if err := saveCfgFunc(cfg); err != nil {
			return nil, fmt.Errorf("unable to save the configuration: %w", err)
		}
	}
	return obs, nil

}

func newTwitch(
	ctx context.Context,
	cfg *streamcontrol.AbstractPlatformConfig,
	setUserData func(context.Context, *streamcontrol.PlatformConfig[twitch.PlatformSpecificConfig, twitch.StreamProfile]) (bool, error),
	saveCfgFunc func(*streamcontrol.AbstractPlatformConfig) error,
	customOAuthHandler twitch.OAuthHandler,
) (
	*twitch.Twitch,
	error,
) {
	platCfg := streamcontrol.ConvertPlatformConfig[twitch.PlatformSpecificConfig, twitch.StreamProfile](
		ctx, cfg,
	)
	if platCfg == nil {
		return nil, fmt.Errorf("twitch config was not found")
	}

	if cfg.Enable != nil && !*cfg.Enable {
		return nil, ErrSkipBackend
	}

	hadSetNewUserData := false
	if platCfg.Config.Channel == "" || platCfg.Config.ClientID == "" || platCfg.Config.ClientSecret == "" {
		ok, err := setUserData(ctx, platCfg)
		if !ok {
			err := saveCfgFunc(&streamcontrol.AbstractPlatformConfig{
				Enable:         platCfg.Enable,
				Config:         platCfg.Config,
				StreamProfiles: streamcontrol.ToAbstractStreamProfiles(platCfg.StreamProfiles),
				Custom:         platCfg.Custom,
			})
			if err != nil {
				logger.Error(ctx, err)
			}
			return nil, ErrSkipBackend
		}
		if err != nil {
			return nil, fmt.Errorf("unable to set user info: %w", err)
		}
		hadSetNewUserData = true
	}

	logger.Debugf(ctx, "twitch config: %#+v", platCfg)
	platCfg.Config.CustomOAuthHandler = customOAuthHandler
	cfg = streamcontrol.ToAbstractPlatformConfig(ctx, platCfg)
	twitch, err := twitch.New(ctx, *platCfg,
		func(c twitch.Config) error {
			return saveCfgFunc(&streamcontrol.AbstractPlatformConfig{
				Enable:         c.Enable,
				Config:         c.Config,
				StreamProfiles: streamcontrol.ToAbstractStreamProfiles(c.StreamProfiles),
				Custom:         c.Custom,
			})
		},
	)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize Twitch client: %w", err)
	}
	if hadSetNewUserData {
		logger.Debugf(ctx, "confirmed new twitch user data, saving it")
		if err := saveCfgFunc(cfg); err != nil {
			return nil, fmt.Errorf("unable to save the configuration: %w", err)
		}
	}
	return twitch, nil
}

func newYouTube(
	ctx context.Context,
	cfg *streamcontrol.AbstractPlatformConfig,
	setUserData func(context.Context, *streamcontrol.PlatformConfig[youtube.PlatformSpecificConfig, youtube.StreamProfile]) (bool, error),
	saveCfgFunc func(*streamcontrol.AbstractPlatformConfig) error,
	customOAuthHandler youtube.OAuthHandler,
) (
	*youtube.YouTube,
	error,
) {
	platCfg := streamcontrol.ConvertPlatformConfig[youtube.PlatformSpecificConfig, youtube.StreamProfile](
		ctx, cfg,
	)
	if platCfg == nil {
		return nil, fmt.Errorf("youtube config was not found")
	}

	if cfg.Enable != nil && !*cfg.Enable {
		return nil, ErrSkipBackend
	}

	hadSetNewUserData := false
	if platCfg.Config.ClientID == "" || platCfg.Config.ClientSecret == "" {
		ok, err := setUserData(ctx, platCfg)
		if !ok {
			err := saveCfgFunc(&streamcontrol.AbstractPlatformConfig{
				Enable:         platCfg.Enable,
				Config:         platCfg.Config,
				StreamProfiles: streamcontrol.ToAbstractStreamProfiles(platCfg.StreamProfiles),
				Custom:         platCfg.Custom,
			})
			if err != nil {
				logger.Error(ctx, err)
			}
			return nil, ErrSkipBackend
		}
		if err != nil {
			return nil, fmt.Errorf("unable to set user info: %w", err)
		}
		hadSetNewUserData = true
	}

	logger.Debugf(ctx, "youtube config: %#+v", platCfg)
	platCfg.Config.CustomOAuthHandler = customOAuthHandler
	cfg = streamcontrol.ToAbstractPlatformConfig(ctx, platCfg)
	yt, err := youtube.New(ctx, *platCfg,
		func(c youtube.Config) error {
			return saveCfgFunc(&streamcontrol.AbstractPlatformConfig{
				Enable:         c.Enable,
				Config:         c.Config,
				StreamProfiles: streamcontrol.ToAbstractStreamProfiles(c.StreamProfiles),
				Custom:         platCfg.Custom,
			})
		},
	)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize YouTube client: %w", err)
	}
	if hadSetNewUserData {
		logger.Debugf(ctx, "confirmed new youtube user data, saving it")
		if err := saveCfgFunc(cfg); err != nil {
			return nil, fmt.Errorf("unable to save the configuration: %w", err)
		}
	}
	return yt, nil
}

func (d *StreamD) initOBSBackend(ctx context.Context) error {
	obs, err := newOBS(
		ctx,
		d.Config.Backends[obs.ID],
		d.UI.InputOBSConnectInfo,
		func(cfg *streamcontrol.AbstractPlatformConfig) error {
			return d.setPlatformConfig(ctx, obs.ID, cfg)
		},
	)
	if err != nil {
		return err
	}
	d.StreamControllers.OBS = obs
	return nil
}

func (d *StreamD) initTwitchBackend(ctx context.Context) error {
	twitch, err := newTwitch(
		ctx,
		d.Config.Backends[twitch.ID],
		d.UI.InputTwitchUserInfo,
		func(cfg *streamcontrol.AbstractPlatformConfig) error {
			return d.setPlatformConfig(ctx, twitch.ID, cfg)
		},
		d.UI.OAuthHandlerTwitch)
	if err != nil {
		return err
	}
	d.StreamControllers.Twitch = twitch
	return nil
}

func (d *StreamD) initYouTubeBackend(ctx context.Context) error {
	youTube, err := newYouTube(
		ctx,
		d.Config.Backends[youtube.ID],
		d.UI.InputYouTubeUserInfo,
		func(cfg *streamcontrol.AbstractPlatformConfig) error {
			return d.setPlatformConfig(ctx, youtube.ID, cfg)
		},
		d.UI.OAuthHandlerYouTube,
	)
	if err != nil {
		return fmt.Errorf("unable to initialize the backend 'YouTube': %w", err)
	}
	d.StreamControllers.YouTube = youTube
	return nil
}
