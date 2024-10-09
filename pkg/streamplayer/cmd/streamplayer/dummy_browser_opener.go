package main

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/streamserver/implementations/xaionaro-go-rtmp/streamserver"
)

type dummyBrowserOpener struct{}

var _ streamserver.BrowserOpener = (*dummyBrowserOpener)(nil)

func (dummyBrowserOpener) OpenURL(ctx context.Context, url string) error {
	return nil
}
