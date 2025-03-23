package streamforward

import (
	"context"

	"github.com/xaionaro-go/recoder"
	xaionarogortmp "github.com/xaionaro-go/recoder/xaionaro-go-rtmp"
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
		func(context.Context) (recoder.Factory, error) {
			return xaionarogortmp.NewFactory(), nil
		},
		platformsController,
	)
}
