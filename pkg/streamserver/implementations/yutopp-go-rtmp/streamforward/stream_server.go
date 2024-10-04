package streamforward

import (
	"context"
	"io"

	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	flvtag "github.com/yutopp/go-flv/tag"
)

type StreamServer interface {
	WaitPubsub(ctx context.Context, appKey types.AppKey) Pubsub
	PubsubNames() types.AppKeys
}

type Sub interface {
	io.Closer
	ClosedChan() <-chan struct{}
}

type Pubsub interface {
	Sub(io.Closer, func(ctx context.Context, flv *flvtag.FlvTag) error) Sub
}
