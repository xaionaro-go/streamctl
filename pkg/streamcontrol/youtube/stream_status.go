package youtube

import (
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	youtube "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube/types"
)

type StreamStatusCustomData = youtube.StreamStatusCustomData

func GetStreamStatusCustomData(in *streamcontrol.StreamStatus) StreamStatusCustomData {
	return youtube.GetStreamStatusCustomData(in)
}
