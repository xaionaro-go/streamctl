package types

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/goccy/go-yaml"
)

type ServerType int

const (
	ServerTypeUndefined = ServerType(iota)
	ServerTypeRTMP
	ServerTypeRTSP
	endOfServerType
)

func (t ServerType) String() string {
	switch t {
	case ServerTypeUndefined:
		return "<undefined>"
	case ServerTypeRTMP:
		return "rtmp"
	case ServerTypeRTSP:
		return "rtsp"
	default:
		return fmt.Sprintf("unexpected_type_%d", t)
	}
}

func (t ServerType) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.String())
}

func ParseServerType(s string) (ServerType, error) {
	for c := ServerTypeUndefined; c < endOfServerType; c++ {
		if c.String() == s {
			return c, nil
		}
	}

	return ServerTypeUndefined, fmt.Errorf("unexpected server type value '%s'", s)
}

func (t *ServerType) UnmarshalJSON(b []byte) error {
	var s string
	err := json.Unmarshal(b, &s)
	if err != nil {
		return err
	}

	c, err := ParseServerType(s)
	if err != nil {
		return err
	}
	*t = c
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

	c, err := ParseServerType(s)
	if err != nil {
		return err
	}
	*t = c
	return nil
}

type ServerHandler interface {
	io.Closer

	Type() ServerType
	ListenAddr() string
}

type StreamDestination struct {
	ID  DestinationID
	URL string
}

type StreamID string

type DestinationID string
