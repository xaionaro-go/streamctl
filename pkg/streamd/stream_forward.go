package streamd

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
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

func (a *platformsControllerAdapter) CheckStreamStarted(
	ctx context.Context,
	destination *url.URL,
) (bool, error) {
	var c streamcontrol.AbstractStreamController
	switch {
	case strings.Contains(destination.Hostname(), "youtube"):
		c = streamcontrol.ToAbstract(a.PlatformsController.YouTube)
	case strings.Contains(destination.Hostname(), "twitch"):
		c = streamcontrol.ToAbstract(a.PlatformsController.Twitch)
	default:
		return false, fmt.Errorf("do not know how to check if the stream started for '%s'", destination.String())
	}

	s, err := c.GetStreamStatus(ctx)
	if err != nil {
		return false, fmt.Errorf("unable to get the stream status: %w", err)
	}

	return s.IsActive, nil
}
