package youtube

import (
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"google.golang.org/api/youtube/v3"
)

type StreamStatusCustomData struct {
	ActiveBroadcasts   []*youtube.LiveBroadcast
	UpcomingBroadcasts []*youtube.LiveBroadcast
	Streams            []*youtube.LiveStream
}

func GetStreamStatusCustomData(in *streamcontrol.StreamStatus) StreamStatusCustomData {
	if in == nil {
		logger.Default().Errorf("nil input")
		return StreamStatusCustomData{}
	}
	r, ok := in.CustomData.(StreamStatusCustomData)
	if !ok {
		logger.Default().Errorf("unexpected type %T", in)
		return StreamStatusCustomData{}
	}
	return r
}
