package types

import (
	"context"
	"net/url"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

type PlatformsController interface {
	CheckStreamStartedByURL(ctx context.Context, destination *url.URL) (bool, error)
	CheckStreamStartedByPlatformID(
		ctx context.Context,
		platID streamcontrol.PlatformName,
	) (bool, error)
}
