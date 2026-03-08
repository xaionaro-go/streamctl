package streamcontrol

import (
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"
)

type StreamIDFullyQualified struct {
	AccountIDFullyQualified
	StreamID StreamID // can never be empty
}

func (id StreamIDFullyQualified) Validate() error {
	if err := id.AccountIDFullyQualified.Validate(); err != nil {
		return fmt.Errorf("invalid account ID: %w", err)
	}
	if id.StreamID == "" {
		return fmt.Errorf("stream ID is empty")
	}
	return nil
}

func NewStreamIDFullyQualified(platID PlatformID, accID AccountID, streamID StreamID) StreamIDFullyQualified {
	id := StreamIDFullyQualified{
		AccountIDFullyQualified: AccountIDFullyQualified{
			PlatformID: platID,
			AccountID:  accID,
		},
		StreamID: streamID,
	}
	if err := id.Validate(); err != nil {
		panic(err)
	}
	return id
}

func (id StreamIDFullyQualified) String() string {
	return fmt.Sprintf("%s:%s:%s", id.PlatformID, id.AccountID, id.StreamID)
}

func (id StreamIDFullyQualified) MarshalYAML() (any, error) {
	return id.String(), nil
}

func (id *StreamIDFullyQualified) UnmarshalYAML(b []byte) error {
	var text string
	if err := yaml.Unmarshal(b, &text); err != nil {
		return err
	}
	return id.UnmarshalText([]byte(text))
}

func (id StreamIDFullyQualified) MarshalText() ([]byte, error) {
	return fmt.Appendf(nil, "%s:%s:%s", id.PlatformID, id.AccountID, id.StreamID), nil
}

func (id *StreamIDFullyQualified) UnmarshalText(text []byte) error {
	s := string(text)
	if strings.Contains(s, "{") {
		s = strings.ReplaceAll(s, "{", "")
		s = strings.ReplaceAll(s, "}", "")
		parts := strings.Fields(s)
		if len(parts) >= 1 {
			id.PlatformID = PlatformID(parts[0])
		}
		if len(parts) >= 2 {
			id.AccountID = AccountID(parts[1])
		}
		if len(parts) >= 3 {
			id.StreamID = StreamID(parts[2])
		}
		return nil
	}
	parts := strings.Split(s, ":")
	if len(parts) < 3 {
		return fmt.Errorf("invalid StreamIDFullyQualified: '%s'", s)
	}
	id.PlatformID = PlatformID(parts[0])
	id.AccountID = AccountID(parts[1])
	id.StreamID = StreamID(parts[2])
	return nil
}

func (id StreamIDFullyQualified) Ptr() *StreamIDFullyQualified {
	return &id
}

func (id *StreamIDFullyQualified) Deref() StreamIDFullyQualified {
	if id == nil {
		return StreamIDFullyQualified{}
	}
	return *id
}
