package streamd

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamd/consts"
)

// defaultOBSMusicLayerName is the OBS scene item name used when
// obs_music_layer_name is not set.
const defaultOBSMusicLayerName = "Music"

func (d *StreamD) initObsMusicToggle(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "initObsMusicToggle")
	defer func() { logger.Debugf(ctx, "/initObsMusicToggle: %v", _err) }()

	ch, err := d.SubscribeToVariable(ctx, consts.VarKeyOBSMusicEnabled)
	if err != nil {
		return fmt.Errorf("unable to subscribe to '%s': %w", consts.VarKeyOBSMusicEnabled, err)
	}

	observability.Go(ctx, func(ctx context.Context) {
		for value := range ch {
			shouldShow := string(value) == "true"
			logger.Debugf(ctx, "obs_music_enabled changed to %v", shouldShow)
			if err := d.setOBSMusicLayerVisible(ctx, shouldShow); err != nil {
				logger.Warnf(ctx, "unable to set OBS music layer visibility to %v: %v", shouldShow, err)
			}
		}
	})

	return nil
}

func (d *StreamD) obsMusicLayerName(
	ctx context.Context,
) string {
	v, err := d.GetVariable(ctx, consts.VarKeyOBSMusicLayerName)
	if err != nil {
		return defaultOBSMusicLayerName
	}
	name := string(v)
	if name == "" {
		return defaultOBSMusicLayerName
	}
	return name
}

func (d *StreamD) setOBSMusicLayerVisible(
	ctx context.Context,
	shouldShow bool,
) error {
	logger.Tracef(ctx, "setOBSMusicLayerVisible(%v)", shouldShow)
	defer logger.Tracef(ctx, "/setOBSMusicLayerVisible(%v)", shouldShow)

	name := d.obsMusicLayerName(ctx)
	return d.OBSElementSetShow(ctx, SceneElementIdentifier{
		Name: &name,
	}, shouldShow)
}
