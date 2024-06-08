package streampanel

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
)

func newTwitch(
	ctx context.Context,
	cfg *streamcontrol.AbstractPlatformConfig,
	setUserData func(context.Context, *streamcontrol.PlatformConfig[twitch.PlatformSpecificConfig, twitch.StreamProfile]) error,
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

	hadSetNewUserData := false
	if platCfg.Config.Channel == "" || platCfg.Config.ClientID == "" || platCfg.Config.ClientSecret == "" {
		if err := setUserData(ctx, platCfg); err != nil {
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
				Config:         c.Config,
				StreamProfiles: streamcontrol.ToAbstractStreamProfiles(c.StreamProfiles),
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
	setUserData func(context.Context, *streamcontrol.PlatformConfig[youtube.PlatformSpecificConfig, youtube.StreamProfile]) error,
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

	hadSetNewUserData := false
	if platCfg.Config.ClientID == "" || platCfg.Config.ClientSecret == "" {
		if err := setUserData(ctx, platCfg); err != nil {
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
				Config:         c.Config,
				StreamProfiles: streamcontrol.ToAbstractStreamProfiles(c.StreamProfiles),
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
