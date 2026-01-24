package streamtypes

import (
	"encoding/json"
	"fmt"
	"strings"
)

type StreamSinkIDFullyQualified struct {
	Type StreamSinkType
	ID   StreamSinkID
}

func NewStreamSinkIDFullyQualified(
	typ StreamSinkType,
	id StreamSinkID,
) StreamSinkIDFullyQualified {
	return StreamSinkIDFullyQualified{
		Type: typ,
		ID:   id,
	}
}

func (s StreamSinkIDFullyQualified) String() string {
	return fmt.Sprintf("%s:%s", s.Type, s.ID)
}

func (s *StreamSinkIDFullyQualified) UnmarshalText(text []byte) error {
	str := string(text)
	parts := strings.SplitN(str, ":", 2)
	if len(parts) == 2 {
		s.Type = ParseStreamSinkType(parts[0])
		if s.Type != UndefinedStreamSinkType {
			s.ID = StreamSinkID(parts[1])
			return nil
		}
	}
	s.Type = StreamSinkTypeCustom
	s.ID = StreamSinkID(str)
	return nil
}

func (s StreamSinkIDFullyQualified) MarshalText() ([]byte, error) {
	return []byte(s.String()), nil
}

func (s *StreamSinkIDFullyQualified) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var str string
	if err := unmarshal(&str); err != nil {
		return err
	}
	return s.UnmarshalText([]byte(str))
}

func (s StreamSinkIDFullyQualified) MarshalYAML() (any, error) {
	return s.String(), nil
}

func (s StreamSinkIDFullyQualified) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

func (s *StreamSinkIDFullyQualified) UnmarshalJSON(b []byte) error {
	var str string
	if err := json.Unmarshal(b, &str); err != nil {
		return err
	}
	return s.UnmarshalText([]byte(str))
}
