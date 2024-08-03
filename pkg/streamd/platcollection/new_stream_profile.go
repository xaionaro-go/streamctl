package platcollection

import (
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
)

func NewStreamProfile(
	platID streamcontrol.PlatformName,
) (streamcontrol.AbstractStreamProfile, error) {
	switch platID {
	case twitch.ID:
		return &twitch.StreamProfile{}, nil
	case youtube.ID:
		return &youtube.StreamProfile{}, nil
	case obs.ID:
		return &obs.StreamProfile{}, nil
	default:
		return nil, fmt.Errorf("unexpected platform ID '%s'", platID)
	}
}
