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
		streamSourceID streamtypes.StreamSourceID,
		waitForNext bool,
	) (<-chan Publisher, error)
}
