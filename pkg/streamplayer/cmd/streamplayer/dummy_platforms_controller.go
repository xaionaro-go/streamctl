package main

import (
	"context"
	"net/url"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/implementations/yutopp-go-rtmp/streamserver"
)

type dummyPlatformsController struct{}

var _ streamserver.PlatformsController = (*dummyPlatformsController)(nil)

func (dummyPlatformsController) CheckStreamStartedByURL(ctx context.Context, destination *url.URL) (bool, error) {
	return true, nil
}

func (dummyPlatformsController) CheckStreamStartedByPlatformID(ctx context.Context, platID streamcontrol.PlatformName) (bool, error) {
	return true, nil
}
