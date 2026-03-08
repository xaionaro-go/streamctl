package streamd

import (
	"context"
	"net/url"
	"time"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

type platformBackendHandler struct {
	InitBackend               func(ctx context.Context, d *StreamD) error
	InitCache                 func(ctx context.Context, d *StreamD) bool
	GetBackendData            func(ctx context.Context, d *StreamD) (any, error)
	OnStartedStream           func(ctx context.Context, d *StreamD)
	OnStartedStreamController func(ctx context.Context, d *StreamD, accountID streamcontrol.AccountID, streamID streamcontrol.StreamID, controller streamcontrol.AbstractAccount) error
	IsPlatformURL             func(u *url.URL) bool
	CheckStreamStarted        func(ctx context.Context, s *streamcontrol.StreamStatus) (bool, error)
	StreamStatusCacheDuration time.Duration
	PostRaidWaitDuration      time.Duration
}

var platformBackendHandlers = make(map[streamcontrol.PlatformID]platformBackendHandler)

func registerPlatformBackendHandler(platID streamcontrol.PlatformID, handler platformBackendHandler) {
	platformBackendHandlers[platID] = handler
}
