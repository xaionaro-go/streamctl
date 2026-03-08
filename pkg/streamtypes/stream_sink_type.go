package streamtypes

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v2"
)

type StreamSinkType int

const (
	UndefinedStreamSinkType = StreamSinkType(iota)
	StreamSinkTypeCustom
	StreamSinkTypeLocal
	StreamSinkTypeExternalPlatform
	endOfStreamSinkType
)

func (s StreamSinkType) String() string {
	switch s {
	case UndefinedStreamSinkType:
		return "<undefined>"
	case StreamSinkTypeCustom:
		return "custom"
	case StreamSinkTypeLocal:
		return "local"
	case StreamSinkTypeExternalPlatform:
		return "external_platform"
	default:
		return fmt.Sprintf("<unknown_%d>", int(s))
	}
}

func ParseStreamSinkType(s string) StreamSinkType {
	s = strings.Trim(strings.ToLower(s), " ")

	for c := UndefinedStreamSinkType; c < endOfStreamSinkType; c++ {
		if c.String() == s {
			return c
		}
	}

	return UndefinedStreamSinkType
}

func (s StreamSinkType) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

func (s *StreamSinkType) UnmarshalJSON(b []byte) error {
	var str string
	if err := json.Unmarshal(b, &str); err != nil {
		return err
	}
	*s = ParseStreamSinkType(str)
	return nil
}

func (s StreamSinkType) MarshalYAML() (any, error) {
	return s.String(), nil
}

func (s *StreamSinkType) UnmarshalYAML(b []byte) error {
	var str string
	if err := yaml.Unmarshal(b, &str); err != nil {
		return err
	}
	*s = ParseStreamSinkType(str)
	return nil
}
