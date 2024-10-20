package streamd

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/andreykaipov/goobs"
	"github.com/andreykaipov/goobs/api/events"
	"github.com/andreykaipov/goobs/api/events/subscriptions"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	streamd "github.com/xaionaro-go/streamctl/pkg/streamd/types"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
)

func (d *StreamD) EXPERIMENTAL_ReinitStreamControllers(ctx context.Context) error {
	platNames := make([]streamcontrol.PlatformName, 0, len(d.Config.Backends))
	for platName := range d.Config.Backends {
		platNames = append(platNames, platName)
	}
	sort.Slice(platNames, func(i, j int) bool {
		return platNames[i] < platNames[j]
	})
	var result *multierror.Error
	for _, platName := range platNames {
		var err error
		switch strings.ToLower(string(platName)) {
		case strings.ToLower(string(obs.ID)):
			err = d.initOBSBackend(ctx)
		case strings.ToLower(string(twitch.ID)):
			err = d.initTwitchBackend(ctx)
		case strings.ToLower(string(kick.ID)):
			err = d.initKickBackend(ctx)
		case strings.ToLower(string(youtube.ID)):
			err = d.initYouTubeBackend(ctx)
		}
		if errors.Is(err, ErrSkipBackend) {
			logger.Debugf(ctx, "backend '%s' is skipped", platName)
			continue
		}
		if err != nil {
			result = multierror.Append(
				result,
				fmt.Errorf("unable to initialize '%s': %w", platName, err),
			)
			continue
		}
		err = d.startListeningForChatMessages(ctx, platName)
		if err != nil {
			logger.Errorf(ctx, "unable to initialize the reader of chat messages for '%s': %w", string(platName), err)
			continue
		}
	}
	return result.ErrorOrNil()
}

var ErrSkipBackend = streamd.ErrSkipBackend

func newOBS(
	ctx context.Context,
	cfg *streamcontrol.AbstractPlatformConfig,
	setConnectionInfo func(context.Context, *streamcontrol.PlatformConfig[obs.PlatformSpecificConfig, obs.StreamProfile]) (bool, error),
	saveCfgFunc func(*streamcontrol.AbstractPlatformConfig) error,
) (
	_ *obs.OBS,
	_err error,
) {
	logger.Debugf(ctx, "newOBS(ctx, %#+v, ...)", cfg)
	defer func() { logger.Debugf(ctx, "/newOBS: %v", _err) }()

	platCfg := streamcontrol.ConvertPlatformConfig[
		obs.PlatformSpecificConfig, obs.StreamProfile,
	](
		ctx, cfg,
	)
	if platCfg == nil {
		return nil, fmt.Errorf("OBS config was not found")
	}

	if cfg.Enable != nil && !*cfg.Enable {
		logger.Debugf(ctx, "skipping OBS, cfg.Enable == %#+v", cfg.Enable)
		return nil, ErrSkipBackend
	}

	hadSetNewConnectionInfo := false
	if platCfg.Config.Host == "" || platCfg.Config.Port == 0 {
		ok, err := setConnectionInfo(ctx, platCfg)
		if !ok {
			logger.Debugf(ctx, "setConnectionInfo commands to skip OBS")
			err := saveCfgFunc(&streamcontrol.AbstractPlatformConfig{
				Enable:         platCfg.Enable,
				Config:         platCfg.Config,
				StreamProfiles: streamcontrol.ToAbstractStreamProfiles(platCfg.StreamProfiles),
			})
			if err != nil {
				logger.Errorf(ctx, "unable to save the config: %v", err)
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
	getOAuthListenPorts func() []uint16,
) (
	*twitch.Twitch,
	error,
) {
	platCfg := streamcontrol.ConvertPlatformConfig[twitch.PlatformSpecificConfig, twitch.StreamProfile](
		ctx,
		cfg,
	)
	if platCfg == nil {
		return nil, fmt.Errorf("twitch config was not found")
	}

	if cfg.Enable != nil && !*cfg.Enable {
		return nil, ErrSkipBackend
	}

	hadSetNewUserData := false
	if platCfg.Config.Channel == "" || platCfg.Config.ClientID == "" ||
		platCfg.Config.ClientSecret == "" {
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
	platCfg.Config.GetOAuthListenPorts = getOAuthListenPorts
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

func newKick(
	ctx context.Context,
	cfg *streamcontrol.AbstractPlatformConfig,
	setUserData func(context.Context, *streamcontrol.PlatformConfig[kick.PlatformSpecificConfig, kick.StreamProfile]) (bool, error),
	saveCfgFunc func(*streamcontrol.AbstractPlatformConfig) error,
	customOAuthHandler kick.OAuthHandler,
	getOAuthListenPorts func() []uint16,
) (
	*kick.Kick,
	error,
) {
	platCfg := streamcontrol.ConvertPlatformConfig[kick.PlatformSpecificConfig, kick.StreamProfile](
		ctx,
		cfg,
	)
	if platCfg == nil {
		return nil, fmt.Errorf("kick config was not found")
	}

	if cfg.Enable != nil && !*cfg.Enable {
		return nil, ErrSkipBackend
	}

	logger.Debugf(ctx, "kick config: %#+v", platCfg)
	cfg = streamcontrol.ToAbstractPlatformConfig(ctx, platCfg)
	kick, err := kick.New(ctx, *platCfg,
		func(c kick.Config) error {
			return saveCfgFunc(&streamcontrol.AbstractPlatformConfig{
				Enable:         c.Enable,
				Config:         c.Config,
				StreamProfiles: streamcontrol.ToAbstractStreamProfiles(c.StreamProfiles),
				Custom:         c.Custom,
			})
		},
	)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize Kick client: %w", err)
	}
	return kick, nil
}

func newYouTube(
	ctx context.Context,
	cfg *streamcontrol.AbstractPlatformConfig,
	setUserData func(context.Context, *streamcontrol.PlatformConfig[youtube.PlatformSpecificConfig, youtube.StreamProfile]) (bool, error),
	saveCfgFunc func(*streamcontrol.AbstractPlatformConfig) error,
	customOAuthHandler youtube.OAuthHandler,
	getOAuthListenPorts func() []uint16,
) (
	*youtube.YouTube,
	error,
) {
	platCfg := streamcontrol.ConvertPlatformConfig[youtube.PlatformSpecificConfig, youtube.StreamProfile](
		ctx,
		cfg,
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
	platCfg.Config.GetOAuthListenPorts = getOAuthListenPorts
	cfg = streamcontrol.ToAbstractPlatformConfig(ctx, platCfg)
	yt, err := youtube.New(ctx, *platCfg,
		func(c youtube.Config) error {
			logger.Debugf(ctx, "saveCfgFunc")
			defer logger.Debugf(ctx, "saveCfgFunc")
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
	if d.StreamControllers.OBS != nil {
		err := d.StreamControllers.OBS.Close()
		if err != nil {
			logger.Warnf(ctx, "unable to close OBS: %v", err)
		}
	}
	d.StreamControllers.OBS = obs
	go d.listenOBSEvents(ctx, obs)
	return nil
}

func (d *StreamD) listenOBSEvents(
	ctx context.Context,
	o *obs.OBS,
) {
	logger.Debugf(ctx, "listenOBSEvents")
	defer logger.Debugf(ctx, "/listenOBSEvents")
	for {
		if o.IsClosed {
			return
		}
		select {
		case <-ctx.Done():
			return
		default:
		}

		client, err := o.GetClient(
			obs.GetClientOption(goobs.WithEventSubscriptions(subscriptions.InputVolumeMeters)),
		)
		if err != nil {
			logger.Errorf(ctx, "unable to get an OBS client: %v", err)
			time.Sleep(time.Second)
			continue
		}

		func() {
			for {
				select {
				case <-ctx.Done():
					return
				case ev, ok := <-client.IncomingEvents:
					if !ok {
						return
					}
					d.processOBSEvent(ctx, ev)
				}
			}
		}()
	}
}

func (d *StreamD) processOBSEvent(
	ctx context.Context,
	ev any,
) {
	logger.Tracef(ctx, "got an OBS event: %T", ev)
	switch ev := ev.(type) {
	case *events.InputVolumeMeters:
		d.OBSState.Do(xsync.WithNoLogging(ctx, true), func() {
			for _, v := range ev.Inputs {
				d.OBSState.VolumeMeters[v.Name] = v.Levels
			}
		})
	}
}

func (d *StreamD) initTwitchBackend(ctx context.Context) error {
	twitch, err := newTwitch(
		ctx,
		d.Config.Backends[twitch.ID],
		d.UI.InputTwitchUserInfo,
		func(cfg *streamcontrol.AbstractPlatformConfig) error {
			return d.setPlatformConfig(ctx, twitch.ID, cfg)
		},
		d.UI.OAuthHandlerTwitch,
		d.GetOAuthListenPorts,
	)
	if err != nil {
		return err
	}
	d.StreamControllers.Twitch = twitch
	return nil
}

func (d *StreamD) initKickBackend(ctx context.Context) error {
	kick, err := newKick(
		ctx,
		d.Config.Backends[kick.ID],
		d.UI.InputKickUserInfo,
		func(cfg *streamcontrol.AbstractPlatformConfig) error {
			return d.setPlatformConfig(ctx, kick.ID, cfg)
		},
		d.UI.OAuthHandlerKick,
		d.GetOAuthListenPorts,
	)
	if err != nil {
		return err
	}
	d.StreamControllers.Kick = kick
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
		d.GetOAuthListenPorts,
	)
	if err != nil {
		return fmt.Errorf("unable to initialize the backend 'YouTube': %w", err)
	}
	d.StreamControllers.YouTube = youTube
	return nil
}
