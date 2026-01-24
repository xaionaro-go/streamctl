package main

import (
	"context"
	"net/url"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	streamservertypes "github.com/xaionaro-go/streamctl/pkg/streamserver/types"
)

type dummyPlatformsController struct{}

var _ streamservertypes.PlatformsController = (*dummyPlatformsController)(nil)

func (dummyPlatformsController) CheckStreamStartedByURL(
	ctx context.Context,
	sinkURL *url.URL,
) (bool, error) {
	return true, nil
}

func (dummyPlatformsController) CheckStreamStartedByPlatformID(
	ctx context.Context,
	platID streamcontrol.PlatformID,
) (bool, error) {
	return true, nil
}

func (dummyPlatformsController) CheckStreamStartedByStreamSourceID(
	ctx context.Context,
	streamSourceID streamcontrol.StreamIDFullyQualified,
) (bool, error) {
	return true, nil
}

func (dummyPlatformsController) WaitStreamStartedByStreamSourceID(
	ctx context.Context,
	streamSourceID streamcontrol.StreamIDFullyQualified,
) error {
	return nil
}

func (dummyPlatformsController) GetActiveStreamIDs(
	ctx context.Context,
) ([]streamcontrol.StreamIDFullyQualified, error) {
	return nil, nil
}

func (dummyPlatformsController) GetStreamSinkConfig(
	ctx context.Context,
	streamID streamcontrol.StreamIDFullyQualified,
) (streamservertypes.StreamSinkConfig, error) {
	return streamservertypes.StreamSinkConfig{}, nil
}
