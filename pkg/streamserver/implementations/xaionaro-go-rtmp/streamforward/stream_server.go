package streamforward

import (
	"context"
	"io"

	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	flvtag "github.com/yutopp/go-flv/tag"
)

type StreamServer interface {
	types.WithConfiger
	types.WaitPublisherChaner
	types.PubsubNameser
	types.GetPortServerser
}

type Sub interface {
	io.Closer
	ClosedChan() <-chan struct{}
}

type Pubsub interface {
	Sub(io.Closer, func(ctx context.Context, flv *flvtag.FlvTag) error) Sub
}
