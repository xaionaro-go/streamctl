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
	destination *url.URL,
) (bool, error) {
	return true, nil
}

func (dummyPlatformsController) CheckStreamStartedByPlatformID(
	ctx context.Context,
	platID streamcontrol.PlatformName,
) (bool, error) {
	return true, nil
}
