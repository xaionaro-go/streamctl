package streamd

import (
	"context"
	"errors"
	"fmt"
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

func (d *StreamD) EXPERIMENTAL_ReinitStreamControllers(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "ReinitStreamControllers")
	defer func() { logger.Debugf(ctx, "/ReinitStreamControllers: %v", _err) }()

	return xsync.DoA1R1(xsync.WithEnableDeadlock(ctx, false), &d.ConfigLock, d.reinitStreamControllers, ctx)
}

func (d *StreamD) reinitStreamControllers(ctx context.Context) error {
	var result *multierror.Error
	for _, platName := range []streamcontrol.PlatformName{
		youtube.ID,
		twitch.ID,
		kick.ID,
		obs.ID,
	} {
		platCfg := d.Config.Backends[platName]
		if platCfg == nil {
			logger.Debugf(ctx, "backend '%s' is not configured", platName)
			continue
		}

		if platCfg.Enable != nil && !*platCfg.Enable {
			logger.Debugf(ctx, "backend '%s' is disabled", platName)
			continue
		}

		if !streamcontrol.IsInitialized(d.Config.Backends, platName) {
			logger.Debugf(ctx, "config of backend '%s' is missing necessary data", platName)
			continue
		}

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
) (
	_ *obs.OBS,
	_err error,
) {
	logger.Debugf(ctx, "newOBS(ctx, %#+v, ...)", cfg)
	defer func() { logger.Debugf(ctx, "/newOBS: %v", _err) }()
	if cfg == nil {
		cfg = &streamcontrol.AbstractPlatformConfig{}
	}
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

	logger.Debugf(ctx, "OBS config: %#+v", platCfg)
	obs, err := obs.New(ctx, *platCfg)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize OBS client: %w", err)
	}
	return obs, nil

}

func newTwitch(
	ctx context.Context,
	cfg *streamcontrol.AbstractPlatformConfig,
	saveCfgFunc func(*streamcontrol.AbstractPlatformConfig) error,
	customOAuthHandler twitch.OAuthHandler,
	getOAuthListenPorts func() []uint16,
) (
	*twitch.Twitch,
	error,
) {
	if cfg == nil {
		cfg = &streamcontrol.AbstractPlatformConfig{}
	}
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

	logger.Debugf(ctx, "twitch config: %#+v", platCfg)
	platCfg.Config.CustomOAuthHandler = customOAuthHandler
	platCfg.Config.GetOAuthListenPorts = getOAuthListenPorts
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
	return twitch, nil
}

func newKick(
	ctx context.Context,
	cfg *streamcontrol.AbstractPlatformConfig,
	saveCfgFunc func(*streamcontrol.AbstractPlatformConfig) error,
	customOAuthHandler kick.OAuthHandler,
	getOAuthListenPorts func() []uint16,
) (
	*kick.Kick,
	error,
) {
	if cfg == nil {
		cfg = &streamcontrol.AbstractPlatformConfig{}
	}
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
	saveCfgFunc func(*streamcontrol.AbstractPlatformConfig) error,
	customOAuthHandler youtube.OAuthHandler,
	getOAuthListenPorts func() []uint16,
) (
	*youtube.YouTube,
	error,
) {
	if cfg == nil {
		cfg = &streamcontrol.AbstractPlatformConfig{}
	}
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

	logger.Debugf(ctx, "youtube config: %#+v", platCfg)
	platCfg.Config.CustomOAuthHandler = customOAuthHandler
	platCfg.Config.GetOAuthListenPorts = getOAuthListenPorts
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
	return yt, nil
}

func (d *StreamD) initOBSBackend(ctx context.Context) error {
	obs, err := newOBS(
		ctx,
		d.Config.Backends[obs.ID],
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
