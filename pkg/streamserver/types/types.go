package types

import (
	"github.com/xaionaro-go/recoder/xaionaro-go-rtmp/yutoppgortmp"
	"github.com/xaionaro-go/streamctl/pkg/secret"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

type StreamSinkID = streamtypes.StreamSinkID
type StreamSinkIDFullyQualified = streamtypes.StreamSinkIDFullyQualified
type StreamSinkType = streamtypes.StreamSinkType

const (
	UndefinedStreamSinkType        = streamtypes.UndefinedStreamSinkType
	StreamSinkTypeCustom           = streamtypes.StreamSinkTypeCustom
	StreamSinkTypeLocal            = streamtypes.StreamSinkTypeLocal
	StreamSinkTypeExternalPlatform = streamtypes.StreamSinkTypeExternalPlatform
)

var NewStreamSinkIDFullyQualified = streamtypes.NewStreamSinkIDFullyQualified

type StreamSink struct {
	ID        StreamSinkIDFullyQualified
	Name      string
	URL       string
	StreamKey secret.String
	StreamID  *streamcontrol.StreamIDFullyQualified
}

type StreamSourceID = streamtypes.StreamSourceID

type AppKey = yutoppgortmp.AppKey

type AppKeys []AppKey

func (s AppKeys) Strings() []string {
	result := make([]string, 0, len(s))
	for _, in := range s {
		result = append(result, string(in))
	}
	return result
}
