package streamforward

import (
	"context"
	"io"

	flvtag "github.com/yutopp/go-flv/tag"
)

type StreamServer interface {
	WaitPubsub(ctx context.Context, appKey string) Pubsub
	PubsubNames() []string
}

type Sub interface {
	io.Closer
	ClosedChan() <-chan struct{}
}

type Pubsub interface {
	Sub(io.Closer, func(ctx context.Context, flv *flvtag.FlvTag) error) Sub
}
