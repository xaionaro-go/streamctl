package xaionarogortmp

import (
	"github.com/xaionaro-go/streamctl/pkg/streamserver/streamforward"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
)

type StreamForwards = streamforward.StreamForwards

func NewStreamForwards(
	s StreamServer,
	platformsController types.PlatformsController,
) *StreamForwards {
	return streamforward.NewStreamForwards(s, NewEncoderFactory(), platformsController)
}
