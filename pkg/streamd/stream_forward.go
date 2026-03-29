package streamd

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/xaionaro-go/streamctl/pkg/clock"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/xsync"
)

type platformsControllerAdapter struct {
	StreamD *StreamD
}

func newPlatformsControllerAdapter(
	streamD *StreamD,
) *platformsControllerAdapter {
	return &platformsControllerAdapter{
		StreamD: streamD,
	}
}

func (a *platformsControllerAdapter) CheckStreamStartedByURL(
	ctx context.Context,
	sinkURL *url.URL,
) (bool, error) {
	platID, ok := xsync.DoR2(ctx, &a.StreamD.AccountsLocker, func() (streamcontrol.PlatformID, bool) {
		for _, account := range a.StreamD.AccountMap {
			if account.IsPlatformURL(sinkURL) {
				return account.GetPlatformID(), true
			}
		}
		return "", false
	})
	if ok {
		return a.CheckStreamStartedByPlatformID(ctx, platID)
	}

	return false, fmt.Errorf(
		"do not know how to check if the stream started for '%s'",
		sinkURL.String(),
	)
}

func (a *platformsControllerAdapter) CheckStreamStartedByPlatformID(
	ctx context.Context,
	platID streamcontrol.PlatformID,
) (bool, error) {
	return a.CheckStreamStartedByStreamSourceID(ctx, streamcontrol.StreamIDFullyQualified{
		AccountIDFullyQualified: streamcontrol.AccountIDFullyQualified{
			PlatformID: platID,
		},
	})
}

func (a *platformsControllerAdapter) CheckStreamStartedByStreamSourceID(
	ctx context.Context,
	streamID streamcontrol.StreamIDFullyQualified,
) (bool, error) {
	s, err := a.StreamD.GetStreamStatus(ctx, streamID)
	if err != nil {
		return false, fmt.Errorf("unable to get the stream status: %w", err)
	}

	if !s.IsActive {
		return false, nil
	}

	handler, ok := platformBackendHandlers[streamID.PlatformID]
	if !ok || handler.CheckStreamStarted == nil {
		return true, nil
	}

	return handler.CheckStreamStarted(ctx, s)
}

func (a *platformsControllerAdapter) WaitStreamStartedByStreamSourceID(
	ctx context.Context,
	streamID streamcontrol.StreamIDFullyQualified,
) error {
	for {
		started, err := a.CheckStreamStartedByStreamSourceID(ctx, streamID)
		if err == nil && started {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-clock.Get().After(time.Second):
		}
	}
}

func (a *platformsControllerAdapter) GetActiveStreamIDs(
	ctx context.Context,
) ([]streamcontrol.StreamIDFullyQualified, error) {
	return a.StreamD.GetActiveStreamIDs(ctx)
}

func (a *platformsControllerAdapter) GetStreamSinkConfig(
	ctx context.Context,
	streamID streamcontrol.StreamIDFullyQualified,
) (types.StreamSinkConfig, error) {
	return a.StreamD.GetStreamSinkConfig(ctx, streamID)
}
