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
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
)

type platformsControllerAdapter struct {
	StreamD api.StreamD
}

func newPlatformsControllerAdapter(
	streamD api.StreamD,
) *platformsControllerAdapter {
	return &platformsControllerAdapter{
		StreamD: streamD,
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
		return false, fmt.Errorf(
			"do not know how to check if the stream started for '%s'",
			destination.String(),
		)
	}
	return a.CheckStreamStartedByPlatformID(ctx, platID)
}

func (a *platformsControllerAdapter) CheckStreamStartedByPlatformID(
	ctx context.Context,
	platID streamcontrol.PlatformName,
) (bool, error) {
	s, err := a.StreamD.GetStreamStatus(ctx, platID)
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

type browserOpenerAdapter struct {
	StreamD *StreamD
}

func newBrowserOpenerAdapter(streamD *StreamD) *browserOpenerAdapter {
	return &browserOpenerAdapter{
		StreamD: streamD,
	}
}

func (a *browserOpenerAdapter) OpenURL(ctx context.Context, url string) error {
	return a.StreamD.UI.OpenBrowser(ctx, url)
}
