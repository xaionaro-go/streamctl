package streamtypes

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v2"
)

type ServerType int

const (
	UndefinedServerType = ServerType(iota)
	ServerTypeSRT
	ServerTypeRTSP
	ServerTypeRTMP
	ServerTypeHLS
	ServerTypeWebRTC
	endOfServerType
)

func (t ServerType) String() string {
	switch t {
	case UndefinedServerType:
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
	s = strings.Trim(strings.ToLower(s), " ")

	for c := UndefinedServerType; c < endOfServerType; c++ {
		if c.String() == s {
			return c
		}
	}

	return UndefinedServerType
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
