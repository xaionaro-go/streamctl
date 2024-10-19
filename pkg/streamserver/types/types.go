package types

import (
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

type StreamDestination struct {
	ID        DestinationID
	URL       string
	StreamKey string
}

type StreamID = streamtypes.StreamID

type DestinationID = streamtypes.DestinationID

type AppKey string

type AppKeys []AppKey

func (s AppKeys) Strings() []string {
	result := make([]string, 0, len(s))
	for _, in := range s {
		result = append(result, string(in))
	}
	return result
}
