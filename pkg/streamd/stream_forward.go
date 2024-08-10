package streamd

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	twitch "github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch/types"
	youtube "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube/types"
)

type platformsControllerAdapter struct {
	PlatformsController StreamControllers
}

func newPlatformsControllerAdapter(
	platformsController StreamControllers,
) *platformsControllerAdapter {
	return &platformsControllerAdapter{
		PlatformsController: platformsController,
	}
}

func (a *platformsControllerAdapter) CheckStreamStartedByURL(
	ctx context.Context,
	destination *url.URL,
) (bool, error) {
	var platID streamcontrol.PlatformName
	switch {
	case strings.Contains(destination.Hostname(), "youtube"):
		platID = youtube.ID
	case strings.Contains(destination.Hostname(), "twitch"):
		platID = twitch.ID
	default:
		return false, fmt.Errorf("do not know how to check if the stream started for '%s'", destination.String())
	}
	return a.CheckStreamStartedByPlatformID(ctx, platID)
}

func (a *platformsControllerAdapter) CheckStreamStartedByPlatformID(
	ctx context.Context,
	platID streamcontrol.PlatformName,
) (bool, error) {
	var c streamcontrol.AbstractStreamController
	switch platID {
	case youtube.ID:
		c = streamcontrol.ToAbstract(a.PlatformsController.YouTube)
	case twitch.ID:
		c = streamcontrol.ToAbstract(a.PlatformsController.Twitch)
	default:
		return false, fmt.Errorf("unknown platform '%s'", platID)
	}

	s, err := c.GetStreamStatus(ctx)
	if err != nil {
		return false, fmt.Errorf("unable to get the stream status: %w", err)
	}

	if !s.IsActive {
		return false, nil
	}

	switch platID {
	case youtube.ID:
		data, ok := s.CustomData.(youtube.StreamStatusCustomData)
		if !ok {
			return true, fmt.Errorf("unexpected type: %T", s.CustomData)
		}
		for _, s := range data.Streams {
			if s == nil {
				continue
			}
			logger.Debugf(ctx, "stream status: %#+v", *s)
			if s.Status.HealthStatus.Status == "good" {
				return true, nil
			}
		}
		return false, nil
	}

	return true, nil
}
