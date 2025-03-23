package streamforward

import (
	"context"

	"github.com/xaionaro-go/recoder"
	"github.com/xaionaro-go/recoder/libav"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/streamforward"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
)

type StreamForwards = streamforward.StreamForwards

func NewStreamForwards(
	s StreamServer,
	platformsController types.PlatformsController,
) *StreamForwards {
	return streamforward.NewStreamForwards(
		s,
		func(ctx context.Context) (recoder.Factory, error) {
			return libav.NewFactory(ctx)
		},
		platformsController,
	)
}
