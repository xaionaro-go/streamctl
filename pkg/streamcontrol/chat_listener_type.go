package streamcontrol

import (
	"encoding/json"
	"fmt"
)

// ChatListenerType categorizes chat listeners using the PACE methodology
// (Primary, Alternate, Contingency, Emergency). Each platform implements
// a subset of these types. Each enabled type runs as its own process for
// fault isolation — if one panics, others keep working.
type ChatListenerType int

const (
	ChatListenerPrimary     = ChatListenerType(iota) // Best available method
	ChatListenerAlternate                            // Same quality, different implementation
	ChatListenerContingency                          // Ugly/deprecated fallback
	ChatListenerEmergency                            // Last resort
	EndOfChatListenerType
)

func (t ChatListenerType) String() string {
	switch t {
	case ChatListenerPrimary:
		return "primary"
	case ChatListenerAlternate:
		return "alternate"
	case ChatListenerContingency:
		return "contingency"
	case ChatListenerEmergency:
		return "emergency"
	default:
		return fmt.Sprintf("unknown_%d", int(t))
	}
}

func (t ChatListenerType) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.String())
}

func ChatListenerTypeFromString(s string) (ChatListenerType, error) {
	for i := range EndOfChatListenerType {
		if s == i.String() {
			return i, nil
		}
	}
	return ChatListenerPrimary, fmt.Errorf("unknown chat listener type: '%v'", s)
}

func (t *ChatListenerType) UnmarshalJSON(data []byte) error {
	var s string
	err := json.Unmarshal(data, &s)
	if err != nil {
		return fmt.Errorf("unable to unmarshal JSON string: %w", err)
	}
	v, err := ChatListenerTypeFromString(s)
	if err != nil {
		return fmt.Errorf("unable to convert string '%s' to ChatListenerType: %w", s, err)
	}
	*t = v
	return nil
}

func (t ChatListenerType) MarshalYAML() (any, error) {
	return t.String(), nil
}

func (t *ChatListenerType) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	v, err := ChatListenerTypeFromString(s)
	if err != nil {
		return err
	}
	*t = v
	return nil
}
