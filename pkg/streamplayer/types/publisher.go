package types

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

type Publisher interface {
	ClosedChan() <-chan struct{}
}

type WaitPublisherChaner interface {
	WaitPublisherChan(
		ctx context.Context,
		streamID streamtypes.StreamID,
		waitForNext bool,
	) (<-chan Publisher, error)
}
