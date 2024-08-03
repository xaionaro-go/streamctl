package main

import (
	"context"
	"net/url"

	"github.com/xaionaro-go/streamctl/pkg/streamserver/implementations/yutopp-go-rtmp/streamserver"
)

type dummyPlatformsController struct{}

var _ streamserver.PlatformsController = (*dummyPlatformsController)(nil)

func (dummyPlatformsController) CheckStreamStarted(ctx context.Context, destination *url.URL) (bool, error) {
	return true, nil
}
