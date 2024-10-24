package streamtypes

import (
	"encoding/json"
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/xsync"
	"gopkg.in/yaml.v2"
)

type StreamID string
type DestinationID string
type OBSInstanceID string

type ServerType int

const (
	ServerTypeUndefined = ServerType(iota)
	ServerTypeRTSP
	ServerTypeSRT
	ServerTypeRTMP
	ServerTypeHLS
	ServerTypeWebRTC
	endOfServerType
)

func (t *ServerType) String() string {
	if t == nil {
		return "null"
	}

	switch *t {
	case ServerTypeUndefined:
		return "<undefined>"
	case ServerTypeRTMP:
		return "rtmp"
	case ServerTypeRTSP:
		return "rtsp"
	case ServerTypeSRT:
		return "srt"
	case ServerTypeHLS:
		return "hls"
	case ServerTypeWebRTC:
		return "webrtc"
	default:
		return fmt.Sprintf("unknown_type_%d", t)
	}
}

func (t ServerType) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.String())
}

func ParseServerType(s string) ServerType {
	for c := ServerTypeUndefined; c < endOfServerType; c++ {
		if c.String() == s {
			return c
		}
	}

	return ServerTypeUndefined
}

func (t *ServerType) UnmarshalJSON(b []byte) error {
	var s string
	err := json.Unmarshal(b, &s)
	if err != nil {
		return err
	}

	*t = ParseServerType(s)
	return nil
}

func (t ServerType) MarshalYAML() ([]byte, error) {
	return yaml.Marshal(t.String())
}

func (t *ServerType) UnmarshalYAML(b []byte) error {
	var s string
	err := yaml.Unmarshal(b, &s)
	if err != nil {
		return err
	}

	*t = ParseServerType(s)
	return nil
}

type OBSState struct {
	xsync.Mutex
	VolumeMeters map[string][][3]float64
}
