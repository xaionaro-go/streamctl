package types

import (
	"context"
	"net/url"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

type PlatformsController interface {
	CheckStreamStartedByURL(ctx context.Context, sinkURL *url.URL) (bool, error)
	CheckStreamStartedByPlatformID(
		ctx context.Context,
		platID streamcontrol.PlatformID,
	) (bool, error)
	CheckStreamStartedByStreamSourceID(
		ctx context.Context,
		streamID streamcontrol.StreamIDFullyQualified,
	) (bool, error)
	WaitStreamStartedByStreamSourceID(
		ctx context.Context,
		streamID streamcontrol.StreamIDFullyQualified,
	) error
	GetActiveStreamIDs(
		ctx context.Context,
	) ([]streamcontrol.StreamIDFullyQualified, error)
	GetStreamSinkConfig(
		ctx context.Context,
		streamID streamcontrol.StreamIDFullyQualified,
	) (StreamSinkConfig, error)
}
